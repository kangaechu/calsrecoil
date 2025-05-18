package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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
)

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
		if endTime.After(now.Add(time.Duration(runAfterMinutes) * time.Hour)) {
			continue // 終了していない
		}
		if strings.Contains(item.Summary, successTag) {
			continue
		}
		if strings.Count(item.Summary, failureTag) >= maxRetries {
			log.Printf("Skipped: %s, already retried %d times", item.Summary, maxRetries)
			continue
		}
		targets = append(targets, item)
	}

	// 並行実行
	var wg sync.WaitGroup
	for _, ev := range targets {
		wg.Add(1)
		go func(event *calendar.Event) {
			defer wg.Done()
			scriptFullPath := path.Join(filepath.Dir(execPath), scriptFile)
			cleanedSummary := strings.ReplaceAll(event.Summary, failureTag, "")
			cleanedSummary = strings.ReplaceAll(cleanedSummary, successTag, "")
			cleanedSummary = strings.TrimSpace(cleanedSummary)
			cmd := exec.Command(scriptFullPath, event.Start.DateTime, event.End.DateTime, cleanedSummary, event.Location, event.Description)
			out, err := cmd.CombinedOutput()
			fmt.Println(string(out))
			var tag string
			if err == nil {
				tag = successTag
				log.Printf("Success: %s", event.Summary)
			} else {
				tag = failureTag
				log.Printf("Failed: %s, err: %v", event.Summary, err)
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
	log.Println("All done.")
}
