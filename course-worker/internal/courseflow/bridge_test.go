package courseflow

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestValidateSavesExamsAndSchedulesNotifications(t *testing.T) {
	china := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, 11, 19, 8, 20, 0, 0, china)
	state := DefaultState()
	state.Revision = 7
	state.Settings.TermStart = "2026-11-16"
	state.Settings.Reminder = true
	state.Settings.Minutes = 10
	state.Settings.CalendarExceptions = []CalendarException{{Date: "2026-11-19", Kind: "makeup", SourceDay: 2}}
	state.Courses = []Course{{ID: "lab", Name: "Lab", Location: "A", Day: 2, Start: "08:30", End: "09:15", WeekStart: 1, WeekEnd: 1, Parity: "all"},
		{ID: "cancelled", Name: "Cancelled", Location: "B", Day: 2, Start: "08:30", End: "09:15", WeekStart: 1, WeekEnd: 1, Parity: "all", ExcludeDates: []string{"2026-11-19"}}}
	state.Exams = []Exam{{ID: "midterm", Name: "Midterm", Date: "2026-11-19", Start: "08:45", End: "09:30", Location: "B"}}
	raw, _ := json.Marshal(state)
	reply := Run(Request{State: raw, Advance: true, Week: "2026-11-16", Export: true}, now)
	if reply.Error != "" || reply.State == nil || reply.State.Revision != 8 || len(reply.State.Exams) != 1 {
		t.Fatalf("state was not saved: %s", reply.Error)
	}
	if len(reply.Reminders) != 2 || reply.Reminders[0].Title != "Lab" || reply.Reminders[0].At != now.Unix() ||
		!strings.HasPrefix(reply.Reminders[1].Title, "考试：") {
		t.Fatalf("incorrect reminders: %+v", reply.Reminders)
	}
	if reply.Week == nil || len(reply.Week.Days) != 7 || len(reply.Week.Days[3].Entries) != 2 || !reply.Week.Days[3].Today || len(reply.Events) != 4 {
		t.Fatalf("incorrect week view or calendar export: %+v", reply.Week)
	}
}

func TestLegacyEventsKeepTheirOriginalWeeks(t *testing.T) {
	original := time.Local
	t.Cleanup(func() { time.Local = original })
	time.Local = time.FixedZone("Asia/Shanghai", 8*60*60)
	start := time.Date(2026, 11, 19, 10, 0, 0, 0, time.Local)
	reply := Run(Request{Legacy: []LegacyEvent{{ID: "source-event", Title: "Lesson", Start: start.Unix(), End: start.Add(time.Hour).Unix()}}}, start)
	if reply.Error != "" || reply.State.Settings.TermStart != "2026-11-16" || len(reply.State.Courses) != 1 ||
		!reply.State.OccursOn(reply.State.Courses[0], start) || reply.State.OccursOn(reply.State.Courses[0], start.AddDate(0, 0, 7)) {
		t.Fatalf("invalid migration: %+v", reply)
	}
}
