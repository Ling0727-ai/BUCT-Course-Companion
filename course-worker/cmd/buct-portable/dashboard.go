package main

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"buct-course-local/worker/internal/courseflow"
	"buct-course-local/worker/internal/portable"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func (c *companion) dashboardVisible() bool {
	return c.tabs != nil && c.tabs.Selected() == c.dashboardTab
}

func (c *companion) buildDashboard(openTasks func()) fyne.CanvasObject {
	links := []struct{ label, address string }{
		{"i北化", "https://i.buct.edu.cn/"},
		{"本科教务", "https://jwglxt.buct.edu.cn/"},
		{"校园邮箱", "https://mail.buct.edu.cn/"},
		{"图书馆", "https://library.buct.edu.cn/"},
		{"校园卡服务", "https://cit.buct.edu.cn/ykt_7863/list.htm"},
		{"校园 VPN", "https://w.buct.edu.cn/"},
	}
	buttons := make([]fyne.CanvasObject, 0, len(links))
	for _, link := range links {
		address, _ := url.Parse(link.address)
		buttons = append(buttons, widget.NewButton(link.label+" ↗", func() {
			if err := c.app.OpenURL(address); err != nil {
				c.setStatus("无法打开系统浏览器。")
			}
		}))
	}
	actions := container.NewGridWithColumns(2,
		widget.NewButton("打开日课表", func() { c.showScheduleDay(time.Time{}) }),
		widget.NewButton("打开待办", openTasks))
	return scroll(actions, c.dashboard, widget.NewCard("校园入口", "", container.NewGridWithColumns(2, buttons...)))
}

func (c *companion) renderDashboard() {
	now := time.Now()
	c.dashboardMinute = now.Unix() / 60
	dateLabel := now.Format("2006年1月2日") + " · " + weekNames[(int(now.Weekday())+6)%7]
	state, err := c.store.Schedule()
	var schedule fyne.CanvasObject
	if err != nil {
		schedule = widget.NewCard("今日安排", "", wrap("无法读取本地课表。"))
	} else {
		view := courseflow.ViewDashboard(state, now)
		dateLabel += " · " + view.TermName
		if view.TermWeek > 0 {
			dateLabel += fmt.Sprintf(" · 第 %d 周", view.TermWeek)
		}
		today, exams := dashboardToday(view.Today, now), c.dashboardExams(view.Exams, now)
		if c.mobile {
			schedule = container.NewVBox(today, exams)
		} else {
			schedule = container.NewGridWithColumns(2, today, exams)
		}
	}
	c.dashboard.Objects = []fyne.CanvasObject{wrap(dateLabel), schedule, c.dashboardTasks(now.Unix())}
	c.dashboard.Refresh()
}

func (c *companion) dashboardTasks(now int64) fyne.CanvasObject {
	tasks, err := c.store.Tasks()
	if err != nil {
		return widget.NewCard("近期截止", "", wrap("无法读取本地任务。"))
	}
	var due []portable.Task
	pending, overdue := 0, 0
	for _, task := range tasks {
		if task.Completed {
			continue
		}
		pending++
		if task.Deadline > 0 && task.Deadline <= now {
			overdue++
		} else if task.Deadline > now && task.Deadline-now <= 3*86400 {
			due = append(due, task)
		}
	}
	items := []fyne.CanvasObject{wrap(fmt.Sprintf("待办 %d 项 · 逾期 %d 项 · 未来 3 天到期 %d 项", pending, overdue, len(due)))}
	for _, task := range due[:min(5, len(due))] {
		deadline := time.Unix(task.Deadline, 0).Format("01/02 15:04")
		detail := kindName(task.Kind) + " · " + task.Subject + " · 截止 " + deadline
		items = append(items, container.NewBorder(nil, nil, nil, widget.NewButton("详情", func() { c.taskDetail(task) }),
			dashboardEntry(task.Title, detail)))
	}
	return widget.NewCard("近期截止", "", container.NewVBox(items...))
}

func dashboardToday(day courseflow.DayView, now time.Time) fyne.CanvasObject {
	var items []fyne.CanvasObject
	nextFound := false
	clock := now.Format("15:04")
	for _, item := range day.Entries {
		title := item.Name
		if item.Exam {
			title = "考试 · " + title
		} else if !nextFound && item.Start > clock {
			title = "下一节 · " + title
			nextFound = true
		}
		items = append(items, dashboardEntry(title, dashboardDetail(item.Start+"–"+item.End, item)))
	}
	if len(items) == 0 {
		items = append(items, wrap("今天没有安排。"))
	}
	return widget.NewCard("今日安排", "", container.NewVBox(items...))
}

func (c *companion) dashboardExams(exams []courseflow.EventView, now time.Time) fyne.CanvasObject {
	var items []fyne.CanvasObject
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for _, exam := range exams {
		date, err := time.ParseInLocation("2006-01-02", exam.Date, now.Location())
		if err != nil {
			continue
		}
		label := "今天"
		if days := int(date.Sub(today) / (24 * time.Hour)); days > 0 {
			label = fmt.Sprintf("还有 %d 天", days)
		}
		timeRange := label + " · " + date.Format("01/02") + " " + exam.Start + "–" + exam.End
		items = append(items, container.NewBorder(nil, nil, nil, widget.NewButton("查看", func() { c.showScheduleDay(date) }),
			dashboardEntry(exam.Name, dashboardDetail(timeRange, exam))))
	}
	if len(items) == 0 {
		items = append(items, wrap("本地课表暂无待考安排。"))
	}
	return widget.NewCard("近期考试", "", container.NewVBox(items...))
}

func dashboardEntry(title, detail string) fyne.CanvasObject {
	name := wrap(title)
	name.TextStyle.Bold = true
	return container.NewVBox(name, wrap(detail))
}

func dashboardDetail(timeRange string, event courseflow.EventView) string {
	parts := []string{timeRange}
	for _, value := range []string{event.Location, event.Teacher} {
		if value = strings.TrimSpace(value); value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, " · ")
}
