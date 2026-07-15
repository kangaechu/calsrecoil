package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/api/option"

	"google.golang.org/api/calendar/v3"
)

const (
	scriptFile = "calsrecoil.sh"
	timeZone   = "Asia/Tokyo"
	successTag = "🆗"
	failureTag = "🆖"
	maxRetries = 5
	logTag     = "[calsrecoil]"
)

// logInfo はjournaldで拾えるよう、統一タグ付きでstderrにログ出力する。
// 個人情報保護の観点から、CALENDAR_ID はここに渡さないこと。
func logInfo(format string, a ...any) {
	fmt.Fprintf(os.Stderr, logTag+" "+format+"\n", a...)
}

// eventTime はイベントの日時（DateTime優先、終日イベントはDate）を返す。
func eventTime(dt *calendar.EventDateTime) string {
	if dt == nil {
		return "?"
	}
	if dt.DateTime != "" {
		return dt.DateTime
	}
	if dt.Date != "" {
		return dt.Date
	}
	return "?"
}

func main() {
	calendarID, ok := os.LookupEnv("CALENDAR_ID") // 環境変数からカレンダーIDを取得
	if !ok {
		log.Fatal("CALENDAR_ID environment variable is not set")
	}

	// 環境変数から実行までの待ち時間を取得
	runAfterMinutes, err := strconv.Atoi(os.Getenv("RUN_AFTER_MINUTES"))
	if err != nil {
		log.Printf("Invalid RUN_AFTER_MINUTES value: %v, set to 0", err)
		runAfterMinutes = 0
	}

	// LOG_LEVEL=debug のときは詳細（スキップ理由・スクリプト出力）も出す。
	// 未指定/その他は通常ログ（サマリ・対象・実行結果）のみ。
	debug := strings.EqualFold(os.Getenv("LOG_LEVEL"), "debug")

	ctx := context.Background()
	execPath, err := os.Executable()
	if err != nil {
		log.Fatalf("Unable to get executable path: %v", err)
	}

	credentalFullPath := path.Join(filepath.Dir(execPath), "service-account.json")
	srv, err := calendar.NewService(ctx, option.WithCredentialsFile(credentalFullPath))
	if err != nil {
		log.Fatalf("Unable to retrieve Calendar client: %v", err)
	}

	// 1週間前〜今
	loc, _ := time.LoadLocation(timeZone)
	now := time.Now().In(loc)
	weekAgo := now.AddDate(0, 0, -7)
	timeMin := weekAgo.Format(time.RFC3339)
	timeMax := now.Format(time.RFC3339)

	// APIで取得
	events, err := srv.Events.List(calendarID).
		ShowDeleted(false).
		SingleEvents(true).
		TimeMin(timeMin).
		TimeMax(timeMax).
		MaxResults(2500).
		OrderBy("startTime").
		Do()
	if err != nil {
		log.Fatalf("Unable to retrieve events: %v", err)
	}

	// 選定に使う時間窓（イベントの終了時刻ベース）
	// 下限: 1週間前 / 上限: now - RUN_AFTER_MINUTES（終了後この分数だけ経過した予定が対象）
	windowStart := weekAgo
	windowEnd := now.Add(-1 * time.Duration(runAfterMinutes) * time.Minute)

	// 条件に合うイベントをフィルタ
	var targets []*calendar.Event
	for _, item := range events.Items {
		// 終了時刻
		var endTimeStr string
		if item.End != nil {
			endTimeStr = item.End.DateTime
			if endTimeStr == "" { // 終日イベントの場合
				endTimeStr = item.End.Date
			}
		}
		if endTimeStr == "" {
			continue
		}
		endTime, err := time.ParseInLocation(time.RFC3339, endTimeStr, loc)
		if err != nil && len(endTimeStr) == 10 { // "YYYY-MM-DD" の場合
			endTime, err = time.ParseInLocation("2006-01-02", endTimeStr, loc)
		}
		if err != nil {
			continue
		}
		if endTime.After(now.Add(-1 * time.Duration(runAfterMinutes) * time.Minute)) {
			continue // 終了していない
		}
		if strings.Contains(item.Summary, successTag) {
			continue
		}
		if strings.Count(item.Summary, failureTag) >= maxRetries {
			if debug {
				logInfo("スキップ: summary=%q リトライ上限(%d回)到達", item.Summary, maxRetries)
			}
			continue
		}
		targets = append(targets, item)
	}

	// 実行サマリ: 取得件数・対象件数・選定窓・run_after を出力（CALENDAR_IDは出さない）
	logInfo("実行開始: calendar取得=%d件 対象=%d件 window(終了時刻)=%s〜%s run_after=%dmin",
		len(events.Items), len(targets),
		windowStart.Format(time.RFC3339), windowEnd.Format(time.RFC3339), runAfterMinutes)

	// 対象と判定した予定を1件ずつ出力
	for _, ev := range targets {
		logInfo("対象: summary=%q station=%s time=%s〜%s",
			ev.Summary, ev.Location, eventTime(ev.Start), eventTime(ev.End))
	}

	// 並行実行
	var wg sync.WaitGroup
	var successCount, failureCount atomic.Int32
	for _, ev := range targets {
		wg.Add(1)
		go func(event *calendar.Event) {
			defer wg.Done()
			scriptFullPath := path.Join(filepath.Dir(execPath), scriptFile)
			cleanedSummary := strings.ReplaceAll(event.Summary, failureTag, "")
			cleanedSummary = strings.ReplaceAll(cleanedSummary, successTag, "")
			cleanedSummary = strings.TrimSpace(cleanedSummary)
			logInfo("実行: summary=%q station=%s scriptを起動", cleanedSummary, event.Location)
			cmd := exec.Command(scriptFullPath, event.Start.DateTime, event.End.DateTime, cleanedSummary, event.Location, event.Description)
			out, err := cmd.CombinedOutput()
			// スクリプト出力はdebug時、または失敗時のみ出力（通常時のノイズ抑制）
			if debug || err != nil {
				fmt.Printf("log for %s: %s\n", event.Summary, string(out))
			}
			var tag string
			if err == nil {
				tag = successTag
				successCount.Add(1)
				logInfo("完了: summary=%q exit=0", cleanedSummary)
			} else {
				tag = failureTag
				failureCount.Add(1)
				exitCode := -1
				var ee *exec.ExitError
				if errors.As(err, &ee) {
					exitCode = ee.ExitCode()
				}
				logInfo("失敗: summary=%q exit=%d err=%v", cleanedSummary, exitCode, err)
			}

			// description追記
			if event.Summary == "" {
				event.Summary = tag
			} else {
				event.Summary = event.Summary + " " + tag
			}
			_, err = srv.Events.Patch(calendarID, event.Id, event).Do()
			if err != nil {
				log.Printf("Update failed for event %s: %v", event.Summary, err)
			}
		}(ev)
	}
	wg.Wait()
	logInfo("実行完了: 対象=%d件 成功=%d 失敗=%d",
		len(targets), successCount.Load(), failureCount.Load())
}
