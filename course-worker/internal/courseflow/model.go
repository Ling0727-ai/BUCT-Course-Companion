// Adapted from CourseFlow internal/schedule/model.go.
package courseflow

import (
	"fmt"
	"strings"
	"time"
)

type Course struct {
	PeriodStart  int      `json:"period_start,omitempty"`
	PeriodEnd    int      `json:"period_end,omitempty"`
	ExcludeDates []string `json:"exclude_dates,omitempty"`
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Teacher      string   `json:"teacher"`
	Location     string   `json:"location"`
	Day          int      `json:"day"`
	Start        string   `json:"start"`
	End          string   `json:"end"`
	WeekStart    int      `json:"week_start"`
	WeekEnd      int      `json:"week_end"`
	Parity       string   `json:"parity"`
	Color        string   `json:"color"`
	Note         string   `json:"note"`
}

type PeriodTime struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type CalendarException struct {
	Date      string `json:"date"`
	Kind      string `json:"kind"`
	SourceDay int    `json:"source_day,omitempty"`
	Note      string `json:"note"`
}

type Settings struct {
	Periods            []PeriodTime        `json:"periods"`
	TermName           string              `json:"term_name"`
	TermStart          string              `json:"term_start"`
	Weeks              int                 `json:"weeks"`
	Reminder           bool                `json:"reminder"`
	Minutes            int                 `json:"minutes"`
	CalendarExceptions []CalendarException `json:"calendar_exceptions,omitempty"`
}

type Exam struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Date     string `json:"date"`
	Start    string `json:"start"`
	End      string `json:"end"`
	Location string `json:"location"`
	Note     string `json:"note"`
}

type State struct {
	Revision int64    `json:"revision"`
	Courses  []Course `json:"courses"`
	Exams    []Exam   `json:"exams,omitempty"`
	Settings Settings `json:"settings"`
}

func DefaultState() State {
	now := time.Now()
	offset := (int(now.Weekday()) + 6) % 7
	return State{Courses: []Course{}, Settings: Settings{TermName: "我的学期", TermStart: now.AddDate(0, 0, -offset).Format("2006-01-02"), Weeks: 20, Minutes: 10, Periods: []PeriodTime{{"08:00", "08:45"}, {"08:55", "09:40"}, {"10:00", "10:45"}, {"10:55", "11:40"}, {"11:50", "12:35"}, {"14:00", "14:45"}, {"14:55", "15:40"}, {"16:00", "16:45"}, {"16:55", "17:40"}, {"19:00", "19:45"}, {"19:55", "20:40"}, {"20:50", "21:35"}}}}
}

// TermWeek returns zero for dates outside the configured term.
func (s Settings) TermWeek(date time.Time) int {
	start, err := time.Parse("2006-01-02", s.TermStart)
	if err != nil {
		return 0
	}
	civil := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	if civil.Before(start) {
		return 0
	}
	week := int(civil.Sub(start).Hours()/24)/7 + 1
	if week > s.Weeks {
		return 0
	}
	return week
}

func (s State) OccursOn(c Course, date time.Time) bool {
	week := s.Settings.TermWeek(date)
	if week == 0 || week < c.WeekStart || week > c.WeekEnd || c.Parity == "odd" && week%2 == 0 || c.Parity == "even" && week%2 == 1 {
		return false
	}
	day := (int(date.Weekday())+6)%7 + 1
	key := date.Format("2006-01-02")
	for _, rule := range s.Settings.CalendarExceptions {
		if rule.Date == key {
			if rule.Kind == "off" {
				return false
			}
			day = rule.SourceDay
		}
	}
	for _, value := range c.ExcludeDates {
		if value == key {
			return false
		}
	}
	return c.Day == day
}

func (s State) Validate() error {
	if s.Revision < 0 || s.Revision >= 1<<53-1 {
		return fmt.Errorf("课表版本无效")
	}
	if len(s.Settings.Periods) < 1 || len(s.Settings.Periods) > 24 {
		return fmt.Errorf("每日节次数应为 1 至 24")
	}
	for i, p := range s.Settings.Periods {
		start, e1 := time.Parse("15:04", p.Start)
		end, e2 := time.Parse("15:04", p.End)
		if e1 != nil || e2 != nil || !end.After(start) || len(p.Start) != 5 || len(p.End) != 5 || (i > 0 && p.Start < s.Settings.Periods[i-1].End) {
			return fmt.Errorf("第 %d 节时间范围异常或重叠", i+1)
		}
	}
	date, err := time.Parse("2006-01-02", s.Settings.TermStart)
	if err != nil || date.Weekday() != time.Monday {
		return fmt.Errorf("学期开始日期应为周一")
	}
	if s.Settings.Weeks < 1 || s.Settings.Weeks > 52 || s.Settings.Minutes < 0 || s.Settings.Minutes > 120 {
		return fmt.Errorf("学期周数或提醒时间超出范围")
	}
	seenDates := map[string]bool{}
	if len(s.Settings.CalendarExceptions) > 366 {
		return fmt.Errorf("校历调整最多 366 条")
	}
	for _, rule := range s.Settings.CalendarExceptions {
		d, e := time.Parse("2006-01-02", rule.Date)
		if e != nil || len(rule.Date) != 10 || d.Before(date) || !d.Before(date.AddDate(0, 0, s.Settings.Weeks*7)) || seenDates[rule.Date] {
			return fmt.Errorf("校历日期无效、重复或不在本学期内")
		}
		seenDates[rule.Date] = true
		if rule.Kind != "off" && rule.Kind != "makeup" || rule.Kind == "makeup" && (rule.SourceDay < 1 || rule.SourceDay > 7) || len(rule.Note) > 640 {
			return fmt.Errorf("校历调整类型或补课星期异常")
		}
	}
	if len(s.Courses) > 1000 {
		return fmt.Errorf("课程数量超过 1000 条")
	}
	seen := map[string]bool{}
	for _, c := range s.Courses {
		if len(c.ExcludeDates) > 366 {
			return fmt.Errorf("停课日期最多 366 个")
		}
		for _, value := range c.ExcludeDates {
			if _, e := time.Parse("2006-01-02", value); e != nil || len(value) != 10 {
				return fmt.Errorf("停课日期格式异常")
			}
		}
		if c.ID == "" || seen[c.ID] || len(c.ID) > 100 {
			return fmt.Errorf("课程标识为空或重复")
		}
		seen[c.ID] = true
		if strings.TrimSpace(c.Name) == "" || len(c.Name) > 240 || len(c.Note) > 4000 {
			return fmt.Errorf("课程名称或备注长度异常")
		}
		if strings.TrimSpace(c.Location) == "" || len(c.Location) > 500 {
			return fmt.Errorf("%s: 请填写上课地点", c.Name)
		}
		start, e1 := time.Parse("15:04", c.Start)
		end, e2 := time.Parse("15:04", c.End)
		if e1 != nil || e2 != nil || !end.After(start) || len(c.Start) != 5 || len(c.End) != 5 {
			return fmt.Errorf("%s: 结束时间须晚于开始时间", c.Name)
		}
		if c.Day < 1 || c.Day > 7 || c.WeekStart < 1 || c.WeekEnd < c.WeekStart || c.WeekEnd > s.Settings.Weeks {
			return fmt.Errorf("%s: 星期或教学周范围异常", c.Name)
		}
		if c.Parity != "all" && c.Parity != "odd" && c.Parity != "even" {
			return fmt.Errorf("%s: 单双周格式异常", c.Name)
		}
	}
	if len(s.Exams) > 1000 {
		return fmt.Errorf("考试数量超过 1000 条")
	}
	seen = map[string]bool{}
	for _, exam := range s.Exams {
		if strings.TrimSpace(exam.ID) == "" || len(exam.ID) > 100 || seen[exam.ID] || strings.TrimSpace(exam.Name) == "" ||
			len(exam.Name) > 240 || len(exam.Location) > 500 || len(exam.Note) > 4000 {
			return fmt.Errorf("考试编号、名称或备注无效")
		}
		seen[exam.ID] = true
		if _, err := time.Parse("2006-01-02", exam.Date); err != nil {
			return fmt.Errorf("考试日期无效")
		}
		start, startErr := time.Parse("15:04", exam.Start)
		end, endErr := time.Parse("15:04", exam.End)
		if startErr != nil || endErr != nil || len(exam.Start) != 5 || len(exam.End) != 5 || !end.After(start) {
			return fmt.Errorf("考试结束时间须晚于开始时间")
		}
	}
	return nil
}
