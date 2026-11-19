package courseflow

import (
	"sort"
	"time"
)

type WeekView struct {
	Days []DayView  `json:"days"`
	Next *EventView `json:"next,omitempty"`
}

type DayView struct {
	Date    string      `json:"date"`
	Today   bool        `json:"today"`
	Entries []EventView `json:"entries"`
}

type EventView struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Date     string `json:"date"`
	Start    string `json:"start"`
	End      string `json:"end"`
	Location string `json:"location"`
	Teacher  string `json:"teacher"`
	Color    string `json:"color,omitempty"`
	Exam     bool   `json:"exam"`
}

// ViewWeek returns the seven days of a week and the next upcoming event.
func ViewWeek(state State, monday, now time.Time) *WeekView {
	view := &WeekView{}
	for offset := 0; offset < 7; offset++ {
		date := monday.AddDate(0, 0, offset)
		key := date.Format("2006-01-02")
		view.Days = append(view.Days, DayView{Date: key, Today: key == now.Format("2006-01-02"), Entries: eventsOn(state, date)})
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for offset := 0; offset < 14 && view.Next == nil; offset++ {
		for _, entry := range eventsOn(state, today.AddDate(0, 0, offset)) {
			start, err := time.ParseInLocation("2006-01-02 15:04", entry.Date+" "+entry.Start, now.Location())
			if err == nil && start.After(now) {
				view.Next = &entry
				break
			}
		}
	}
	return view
}

func eventsOn(state State, date time.Time) []EventView {
	key := date.Format("2006-01-02")
	items := []EventView{}
	for _, c := range state.Courses {
		if state.OccursOn(c, date) {
			items = append(items, EventView{ID: c.ID, Name: c.Name, Date: key, Start: c.Start, End: c.End, Location: c.Location, Teacher: c.Teacher, Color: c.Color})
		}
	}
	for _, e := range state.Exams {
		if e.Date == key {
			items = append(items, EventView{ID: e.ID, Name: e.Name, Date: key, Start: e.Start, End: e.End, Location: e.Location, Exam: true})
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Start < items[j].Start })
	return items
}
