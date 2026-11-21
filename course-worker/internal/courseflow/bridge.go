package courseflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"time"
)

type Request struct {
	State     json.RawMessage `json:"state"`
	Legacy    []LegacyEvent   `json:"legacy"`
	Advance   bool            `json:"advance"`
	Week      string          `json:"week"`
	Export    bool            `json:"export"`
	Dashboard bool            `json:"dashboard"`
}

type Reply struct {
	State     *State         `json:"state,omitempty"`
	Reminders []Notice       `json:"reminders,omitempty"`
	Error     string         `json:"error,omitempty"`
	Week      *WeekView      `json:"week,omitempty"`
	Events    []EventView    `json:"events,omitempty"`
	Dashboard *DashboardView `json:"dashboard,omitempty"`
}

type Notice struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Body  string `json:"body"`
	At    int64  `json:"at"`
}

func Run(request Request, now time.Time) Reply {
	state := DefaultState()
	if len(request.State) > 0 && string(request.State) != "null" {
		decoder := json.NewDecoder(bytes.NewReader(request.State))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&state); err != nil || !errors.Is(decoder.Decode(new(any)), io.EOF) {
			return Reply{Error: "课表数据格式异常。"}
		}
	} else if len(request.Legacy) > 0 {
		var err error
		state, err = fromLegacy(request.Legacy)
		if err != nil {
			return Reply{Error: err.Error()}
		}
	}
	if err := state.Validate(); err != nil {
		return Reply{Error: err.Error()}
	}
	if request.Advance {
		state.Revision++
	}
	if state.Courses == nil {
		state.Courses = []Course{}
	}
	reply := Reply{State: &state, Reminders: UpcomingNotices(state, now)}
	if request.Dashboard {
		view := ViewDashboard(state, now)
		reply.Dashboard = &view
	}
	if request.Week != "" {
		monday, err := time.ParseInLocation("2006-01-02", request.Week, now.Location())
		if err != nil || monday.Weekday() != time.Monday {
			return Reply{Error: "课表周起始日期无效。"}
		}
		reply.Week = ViewWeek(state, monday, now)
	}
	if request.Export {
		first, _ := time.ParseInLocation("2006-01-02", state.Settings.TermStart, now.Location())
		end := first.AddDate(0, 0, state.Settings.Weeks*7)
		for day := first; day.Before(end); day = day.AddDate(0, 0, 1) {
			reply.Events = append(reply.Events, eventsOn(state, day)...)
		}
		for _, exam := range state.Exams {
			day, _ := time.ParseInLocation("2006-01-02", exam.Date, now.Location())
			if day.Before(first) || !day.Before(end) {
				reply.Events = append(reply.Events, exam.event())
			}
		}
	}
	return reply
}

// The native host refreshes the queue daily. CourseFlow's OccursOn also
// accounts for term weeks, holidays, make-up days and single cancellations.
func UpcomingNotices(state State, now time.Time) []Notice {
	if !state.Settings.Reminder {
		return nil
	}
	var notices []Notice
	lead := time.Duration(state.Settings.Minutes) * time.Minute
	begin := now.Add(-5 * time.Minute)
	end := now.Add(36 * time.Hour)
	add := func(id, title, location, clock string, date time.Time) {
		parsed, err := time.Parse("15:04", clock)
		if err != nil {
			return
		}
		starts := time.Date(date.Year(), date.Month(), date.Day(), parsed.Hour(), parsed.Minute(), 0, 0, now.Location())
		at := starts.Add(-lead)
		if at.Before(begin) || !at.Before(end) || !starts.After(now) {
			return
		}
		notices = append(notices, Notice{Key: id + "@" + starts.Format(time.RFC3339), Title: title,
			Body: clock + " · " + location, At: at.Unix()})
	}
	first := time.Date(begin.Year(), begin.Month(), begin.Day(), 0, 0, 0, 0, now.Location())
	for day := first; day.Before(end.Add(lead)); day = day.AddDate(0, 0, 1) {
		for _, course := range state.Courses {
			if state.OccursOn(course, day) {
				add("course:"+course.ID, course.Name, course.Location, course.Start, day)
			}
		}
	}
	for _, exam := range state.Exams {
		day, err := time.ParseInLocation("2006-01-02", exam.Date, now.Location())
		if err == nil {
			add("exam:"+exam.ID, "考试："+exam.Name, exam.Location, exam.Start, day)
		}
	}
	sort.SliceStable(notices, func(i, j int) bool { return notices[i].At < notices[j].At })
	return notices
}
