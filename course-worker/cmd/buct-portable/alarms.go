package main

import (
	"fmt"
	"sort"
	"time"

	"buct-course-local/worker/internal/courseflow"
	"buct-course-local/worker/internal/portable"
	"fyne.io/fyne/v2"
)

type nextAlarm struct {
	key, title, body string
	at               int64
}

func taskAlarmKey(t portable.Task, hours int) string {
	return fmt.Sprintf("task:%s:%d:%d", t.ID, t.Deadline, hours)
}

func (c *companion) reconcileMobileAlarms(now time.Time, tasks []portable.Task, state courseflow.State, notifyTasks bool) map[string]bool {
	stored, err := c.store.Alarms()
	if err != nil {
		return nil
	}
	unix := now.Unix()
	end := now.Add(36 * time.Hour).Unix()
	wanted := make(map[string]nextAlarm)
	if notifyTasks {
		for _, t := range tasks {
			if t.Completed {
				continue
			}
			for _, h := range []int{24, 12, 1} {
				at := t.Deadline - int64(h*3600)
				if at > unix+2 && at <= end && !c.store.TaskNoticeSent(t, h) {
					key := taskAlarmKey(t, h)
					wanted[key] = nextAlarm{key, fmt.Sprintf("%d 小时内截止", h), t.Title, at}
				}
			}
		}
	}
	for _, notice := range courseflow.UpcomingNotices(state, now) {
		if notice.At > unix+2 && !c.store.ScheduleNoticeSent(notice.Key) {
			key := "schedule:" + notice.Key
			wanted[key] = nextAlarm{key, notice.Title, notice.Body, notice.At}
		}
	}
	ordered := make([]nextAlarm, 0, len(wanted))
	for _, alarm := range wanted {
		ordered = append(ordered, alarm)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].at == ordered[j].at {
			return ordered[i].key < ordered[j].key
		}
		return ordered[i].at < ordered[j].at
	})
	for _, alarm := range ordered[min(len(ordered), 60):] {
		delete(wanted, alarm.key)
	}

	posted := make(map[string]bool)
	for _, alarm := range stored {
		if alarm.At <= unix {
			if unix-alarm.At <= 86400 {
				posted[alarm.Key] = true
			} else {
				_ = c.store.DropAlarm(alarm.Key)
			}
			continue
		}
		if target, ok := wanted[alarm.Key]; ok && target.at == alarm.At {
			delete(wanted, alarm.Key)
			continue
		}
		if err := c.app.CancelScheduledNotification(alarm.ID); err == nil {
			_ = c.store.DropAlarm(alarm.Key)
		}
	}
	for _, alarm := range wanted {
		native, err := c.app.ScheduleNotification(fyne.NewNotification(alarm.title, alarm.body), time.Unix(alarm.at, 0))
		if err != nil || native == nil {
			continue
		}
		if err = c.store.SaveAlarm(portable.Alarm{Key: alarm.key, ID: native.ID(), At: alarm.at}); err != nil {
			_ = c.app.CancelScheduledNotification(native.ID())
		}
	}
	return posted
}
