package courseflow

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"
)

type LegacyEvent struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Start       int64  `json:"start"`
	End         int64  `json:"end"`
	Location    string `json:"location"`
	Description string `json:"description"`
}

func fromLegacy(events []LegacyEvent) (State, error) {
	state := DefaultState()
	var first time.Time
	for _, event := range events {
		start := time.Unix(event.Start, 0).In(time.Local)
		if first.IsZero() || start.Before(first) {
			first = start
		}
	}
	first = time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, time.Local)
	first = first.AddDate(0, 0, -((int(first.Weekday()) + 6) % 7))
	state.Settings.TermStart = first.Format("2006-01-02")
	for _, event := range events {
		start := time.Unix(event.Start, 0).In(time.Local)
		end := time.Unix(event.End, 0).In(time.Local)
		week := int(start.Sub(first)/(7*24*time.Hour)) + 1
		if week > 52 || start.Format("2006-01-02") != end.Format("2006-01-02") {
			return State{}, errors.New("旧课表含跨日事件或跨越 52 周，请按学期导入。")
		}
		location := event.Location
		if strings.TrimSpace(location) == "" {
			location = "地点待定"
		}
		id := fmt.Sprintf("legacy-%x", sha256.Sum256([]byte(event.ID)))
		state.Courses = append(state.Courses, Course{ID: id, Name: event.Title, Location: location,
			Day: (int(start.Weekday())+6)%7 + 1, Start: start.Format("15:04"), End: end.Format("15:04"),
			WeekStart: week, WeekEnd: week, Parity: "all", Color: "mint", Note: event.Description})
		state.Settings.Weeks = max(state.Settings.Weeks, week)
	}
	return state, nil
}
