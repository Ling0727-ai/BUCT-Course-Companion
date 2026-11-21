package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"buct-course-local/worker/internal/courseflow"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

var weekNames = []string{"周一", "周二", "周三", "周四", "周五", "周六", "周日"}

func mondayOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()).AddDate(0, 0, -(int(t.Weekday())+6)%7)
}

func (c *companion) buildSchedule() fyne.CanvasObject {
	move := func(step int) {
		if !c.scheduleDayView {
			step *= 7
		}
		c.scheduleDate = c.selectedScheduleDate().AddDate(0, 0, step)
		c.renderSchedule()
	}
	c.schedulePrevious = widget.NewButton("上一周", func() { move(-1) })
	c.scheduleToday = widget.NewButton("本周", func() { c.scheduleDate = time.Time{}; c.renderSchedule() })
	c.scheduleNext = widget.NewButton("下一周", func() { move(1) })
	c.scheduleMode = widget.NewButton("日视图", func() { c.scheduleDayView = !c.scheduleDayView; c.renderSchedule() })
	nav := container.NewGridWithColumns(4, c.schedulePrevious, c.scheduleToday, c.scheduleNext, c.scheduleMode)
	edit := container.NewGridWithColumns(3, widget.NewButton("新建课程", func() { c.editLesson("") }),
		widget.NewButton("新建考试", func() { c.editExam("") }), widget.NewButton("学期与作息", c.editTerm))
	files := container.NewGridWithColumns(2, widget.NewButton("导入课表", c.importSchedule), widget.NewButton("导出课表", c.exportMenu))
	return container.NewBorder(container.NewVBox(nav, edit, files), nil, nil, nil, container.NewVScroll(c.schedule))
}

func (c *companion) selectedScheduleDate() time.Time {
	if c.scheduleDate.IsZero() {
		return time.Now()
	}
	return c.scheduleDate
}

func (c *companion) showScheduleDay(date time.Time) {
	c.scheduleDate = date
	c.scheduleDayView = true
	c.tabs.Select(c.scheduleTab)
}

func (c *companion) renderSchedule() {
	state, err := c.store.Schedule()
	if err != nil {
		c.setStatus("无法读取本地课表。")
		return
	}
	date := c.selectedScheduleDate()
	monday := mondayOf(date)
	view := courseflow.ViewWeek(state, monday, time.Now())
	period := monday.Format("2006/01/02") + "–" + monday.AddDate(0, 0, 6).Format("01/02")
	previous, today, next, mode := "上一周", "本周", "下一周", "日视图"
	if c.scheduleDayView {
		period = date.Format("2006/01/02")
		previous, today, next, mode = "前一天", "今天", "后一天", "周视图"
	}
	c.schedulePrevious.SetText(previous)
	c.scheduleToday.SetText(today)
	c.scheduleNext.SetText(next)
	c.scheduleMode.SetText(mode)
	term := state.Settings.TermName
	if week := state.Settings.TermWeek(date); week > 0 {
		term += fmt.Sprintf(" · 第 %d 周", week)
	}
	term += " · " + period
	head := widget.NewLabelWithStyle(term, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	items := []fyne.CanvasObject{head}
	if view.Next != nil {
		items = append(items, wrap("下一项 · "+view.Next.Date+" "+view.Next.Start+" · "+view.Next.Name))
	}
	var days []fyne.CanvasObject
	for index, day := range view.Days {
		if c.scheduleDayView && day.Date != date.Format("2006-01-02") {
			continue
		}
		label := weekNames[index] + " " + day.Date[5:]
		if day.Today {
			label += " · 今天"
		}
		var entries []fyne.CanvasObject
		for _, event := range day.Entries {
			e := event
			title := e.Name
			if e.Exam {
				title = "考试 · " + title
			}
			info := wrap(e.Start + "–" + e.End + "\n" + strings.TrimSuffix(strings.TrimSpace(e.Location+" "+e.Teacher), " "))
			action := widget.NewButton("编辑", func() {
				if e.Exam {
					c.editExam(e.ID)
				} else {
					c.editLesson(e.ID)
				}
			})
			entries = append(entries, widget.NewCard(title, "", container.NewVBox(info, action)))
		}
		for _, rule := range state.Settings.CalendarExceptions {
			if rule.Date == day.Date {
				if rule.Kind == "off" {
					label += " · 停课"
				} else if rule.SourceDay > 0 && rule.SourceDay <= 7 {
					label += " · 补" + weekNames[rule.SourceDay-1]
				}
			}
		}
		if len(entries) == 0 {
			entries = append(entries, widget.NewLabel("无安排"))
		}
		days = append(days, widget.NewCard(label, "", container.NewVBox(entries...)))
	}
	if c.mobile || c.scheduleDayView {
		items = append(items, days...)
	} else {
		items = append(items, container.NewGridWithColumns(7, days...))
	}
	c.schedule.Objects = items
	c.schedule.Refresh()
}

func entry(text string) *widget.Entry { field := widget.NewEntry(); field.SetText(text); return field }
func form(label string, control fyne.CanvasObject) *widget.FormItem {
	return widget.NewFormItem(label, control)
}

func (c *companion) editLesson(id string) {
	state, err := c.store.Schedule()
	if err != nil {
		c.setStatus("无法读取课表。")
		return
	}
	lesson := courseflow.Course{ID: newID("course-"), Day: 1, Start: "08:00", End: "08:45", WeekStart: 1, WeekEnd: state.Settings.Weeks, Parity: "all", Color: "mint"}
	if c.scheduleDayView {
		lesson.Day = (int(c.selectedScheduleDate().Weekday())+6)%7 + 1
	}
	for _, candidate := range state.Courses {
		if candidate.ID == id {
			lesson = candidate
			break
		}
	}
	name, teacher, location := entry(lesson.Name), entry(lesson.Teacher), entry(lesson.Location)
	day := widget.NewSelect(weekNames, nil)
	day.SetSelectedIndex(lesson.Day - 1)
	start, end := entry(lesson.Start), entry(lesson.End)
	period := entry("")
	period.SetPlaceHolder("如 1-2，留空以钟点为准")
	if lesson.PeriodStart > 0 {
		period.SetText(fmt.Sprintf("%d-%d", lesson.PeriodStart, lesson.PeriodEnd))
	}
	weeks := entry(fmt.Sprintf("%d-%d", lesson.WeekStart, lesson.WeekEnd))
	parity := widget.NewSelect([]string{"每周", "单周", "双周"}, nil)
	parity.SetSelected(map[string]string{"all": "每周", "odd": "单周", "even": "双周"}[lesson.Parity])
	excluded := entry(strings.Join(lesson.ExcludeDates, ","))
	excluded.SetPlaceHolder("YYYY-MM-DD，逗号分隔")
	note := entry(lesson.Note)
	items := []*widget.FormItem{form("课程名称", name), form("地点", location), form("教师", teacher), form("星期", day), form("起止节次", period),
		form("开始时间", start), form("结束时间", end), form("教学周", weeks), form("单双周", parity), form("单次停课日", excluded), form("备注", note)}
	if id != "" {
		items = append(items, form("", widget.NewButton("删除课程", func() {
			dialog.ShowConfirm("删除课程", "确定删除这门课？", func(ok bool) {
				if ok {
					state.Courses = withoutLesson(state.Courses, id)
					c.saveSchedule(state)
				}
			}, c.win)
		})))
	}
	c.showForm("课程安排", items, func() {
		first, last, ok := parseRange(weeks.Text)
		if !ok {
			showError(c.win, "教学周格式应为 1-16。")
			return
		}
		lesson.Name = strings.TrimSpace(name.Text)
		lesson.Teacher = strings.TrimSpace(teacher.Text)
		lesson.Location = strings.TrimSpace(location.Text)
		lesson.Day = day.SelectedIndex() + 1
		lesson.WeekStart = first
		lesson.WeekEnd = last
		lesson.Note = note.Text
		lesson.Start = strings.TrimSpace(start.Text)
		lesson.End = strings.TrimSpace(end.Text)
		lesson.Parity = map[string]string{"每周": "all", "单周": "odd", "双周": "even"}[parity.Selected]
		lesson.ExcludeDates = strings.FieldsFunc(excluded.Text, func(r rune) bool { return r == ',' || r == '，' || r == '\n' || r == ' ' })
		lesson.PeriodStart = 0
		lesson.PeriodEnd = 0
		if strings.TrimSpace(period.Text) != "" {
			a, b, isRange := parseRange(period.Text)
			if !isRange || b > len(state.Settings.Periods) {
				showError(c.win, "节次超出当前作息表。")
				return
			}
			lesson.PeriodStart = a
			lesson.PeriodEnd = b
			lesson.Start = state.Settings.Periods[a-1].Start
			lesson.End = state.Settings.Periods[b-1].End
		}
		state.Courses = append(withoutLesson(state.Courses, lesson.ID), lesson)
		c.saveSchedule(state)
	})
}

func parseRange(raw string) (int, int, bool) {
	parts := strings.Split(strings.TrimSpace(raw), "-")
	if len(parts) != 2 {
		return 0, 0, false
	}
	a, e := strconv.Atoi(strings.TrimSpace(parts[0]))
	b, f := strconv.Atoi(strings.TrimSpace(parts[1]))
	return a, b, e == nil && f == nil && a >= 1 && b >= a
}

func withoutLesson(items []courseflow.Course, id string) []courseflow.Course {
	var result []courseflow.Course
	for _, item := range items {
		if item.ID != id {
			result = append(result, item)
		}
	}
	return result
}

func (c *companion) editExam(id string) {
	state, err := c.store.Schedule()
	if err != nil {
		c.setStatus("无法读取课表。")
		return
	}
	exam := courseflow.Exam{ID: newID("exam-"), Date: time.Now().Format("2006-01-02"), Start: "09:00", End: "11:00"}
	if c.scheduleDayView {
		exam.Date = c.selectedScheduleDate().Format("2006-01-02")
	}
	for _, item := range state.Exams {
		if item.ID == id {
			exam = item
			break
		}
	}
	name, date, location := entry(exam.Name), entry(exam.Date), entry(exam.Location)
	start, end, note := entry(exam.Start), entry(exam.End), entry(exam.Note)
	without := func() []courseflow.Exam {
		var list []courseflow.Exam
		for _, item := range state.Exams {
			if item.ID != exam.ID {
				list = append(list, item)
			}
		}
		return list
	}
	items := []*widget.FormItem{form("考试名称", name), form("日期", date), form("开始", start), form("结束", end), form("考场", location), form("备注", note)}
	if id != "" {
		items = append(items, form("", widget.NewButton("删除考试", func() {
			dialog.ShowConfirm("删除考试", "确定删除这项考试？", func(ok bool) {
				if ok {
					state.Exams = without()
					c.saveSchedule(state)
				}
			}, c.win)
		})))
	}
	c.showForm("考试安排", items, func() {
		exam.Name = strings.TrimSpace(name.Text)
		exam.Date = strings.TrimSpace(date.Text)
		exam.Start = strings.TrimSpace(start.Text)
		exam.End = strings.TrimSpace(end.Text)
		exam.Location = strings.TrimSpace(location.Text)
		exam.Note = note.Text
		state.Exams = append(without(), exam)
		c.saveSchedule(state)
	})
}

func (c *companion) editTerm() {
	state, err := c.store.Schedule()
	if err != nil {
		c.setStatus("无法读取课表。")
		return
	}
	settings := state.Settings
	name, begin := entry(settings.TermName), entry(settings.TermStart)
	weeks, minutes := entry(strconv.Itoa(settings.Weeks)), entry(strconv.Itoa(settings.Minutes))
	remind := widget.NewCheck("课程和考试提醒", nil)
	remind.SetChecked(settings.Reminder)
	periods, exceptions := widget.NewMultiLineEntry(), widget.NewMultiLineEntry()
	periods.SetMinRowsVisible(3)
	exceptions.SetMinRowsVisible(2)
	var clocks, rules []string
	for _, time := range settings.Periods {
		clocks = append(clocks, time.Start+"-"+time.End)
	}
	for _, rule := range settings.CalendarExceptions {
		rules = append(rules, fmt.Sprintf("%s,%s,%d,%s", rule.Date, rule.Kind, rule.SourceDay, rule.Note))
	}
	periods.SetText(strings.Join(clocks, "\n"))
	exceptions.SetText(strings.Join(rules, "\n"))
	exceptions.SetPlaceHolder("日期,off,0 或 日期,makeup,星期1-7")
	c.showForm("学期与作息", []*widget.FormItem{form("学期名", name), form("第一周周一", begin), form("周数", weeks), form("提醒设置", remind),
		form("提前分钟", minutes), form("作息 08:00-08:45", periods), form("校历调整", exceptions)}, func() {
		count, e1 := strconv.Atoi(strings.TrimSpace(weeks.Text))
		lead, e2 := strconv.Atoi(strings.TrimSpace(minutes.Text))
		if e1 != nil || e2 != nil {
			showError(c.win, "请填写有效的周数和提前分钟。")
			return
		}
		settings.TermName = strings.TrimSpace(name.Text)
		settings.TermStart = strings.TrimSpace(begin.Text)
		settings.Weeks = count
		settings.Minutes = lead
		settings.Reminder = remind.Checked
		settings.Periods = nil
		settings.CalendarExceptions = nil
		for _, raw := range strings.Split(periods.Text, "\n") {
			if raw = strings.TrimSpace(raw); raw == "" {
				continue
			}
			a, b, ok := strings.Cut(raw, "-")
			if !ok {
				showError(c.win, "作息每行填写一个起止时间。")
				return
			}
			settings.Periods = append(settings.Periods, courseflow.PeriodTime{Start: strings.TrimSpace(a), End: strings.TrimSpace(b)})
		}
		for _, raw := range strings.Split(exceptions.Text, "\n") {
			if raw = strings.TrimSpace(raw); raw == "" {
				continue
			}
			parts := strings.SplitN(raw, ",", 4)
			if len(parts) < 2 {
				showError(c.win, "校历调整每行填写日期和 off 或 makeup。")
				return
			}
			rule := courseflow.CalendarException{Date: strings.TrimSpace(parts[0]), Kind: strings.TrimSpace(parts[1])}
			if len(parts) > 2 {
				rule.SourceDay, _ = strconv.Atoi(strings.TrimSpace(parts[2]))
			}
			if len(parts) > 3 {
				rule.Note = strings.TrimSpace(parts[3])
			}
			settings.CalendarExceptions = append(settings.CalendarExceptions, rule)
		}
		for i, lesson := range state.Courses {
			if lesson.PeriodStart > 0 {
				if lesson.PeriodEnd < lesson.PeriodStart || lesson.PeriodEnd > len(settings.Periods) {
					showError(c.win, "有课程引用了新作息表中不存在的节次。")
					return
				}
				state.Courses[i].Start = settings.Periods[lesson.PeriodStart-1].Start
				state.Courses[i].End = settings.Periods[lesson.PeriodEnd-1].End
			}
		}
		state.Settings = settings
		c.saveSchedule(state)
	})
}

func (c *companion) saveSchedule(state courseflow.State) {
	if err := c.store.SaveSchedule(state); err != nil {
		showError(c.win, err.Error())
		return
	}
	c.setStatus("课表已保存。")
	c.renderSchedule()
	c.checkNotices()
}
