package main

import (
	"context"
	"google.golang.org/api/option"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"google.golang.org/api/calendar/v3"
)

const (
	scriptFile = "calsrecoil.sh"
	timeZone   = "Asia/Tokyo"
)

func main() {
	calendarId, ok := os.LookupEnv("CALENDAR_ID") // 環境変数からカレンダーIDを取得
	if !ok {
		log.Fatal("CALENDAR_ID environment variable is not set")
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
	events, err := srv.Events.List(calendarId).
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
		if endTime.After(now) {
			continue // 終了していない
		}
		desc := strings.ToLower(item.Description)
		if strings.Contains(desc, "[recorded]") || strings.Contains(desc, "[failed]") {
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
			// 任意の引数で実行（ここではIDを渡す例）
			scriptFullPath := path.Join(filepath.Dir(execPath), scriptFile)
			cmd := exec.Command(scriptFullPath, event.Id)
			err = cmd.Run()
			var tag string
			if err == nil {
				tag = "[recorded]"
				log.Printf("Success: %s", event.Summary)
			} else {
				tag = "[failed]"
				log.Printf("Failed: %s, err: %v", event.Summary, err)
			}

			// description追記
			if event.Description == "" {
				event.Description = tag
			} else {
				event.Description = event.Description + " " + tag
			}
			_, err = srv.Events.Patch(calendarId, event.Id, event).Do()
			if err != nil {
				log.Printf("Update failed for event %s: %v", event.Summary, err)
			}
		}(ev)
	}
	wg.Wait()
	log.Println("All done.")
}
