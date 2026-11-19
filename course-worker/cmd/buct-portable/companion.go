package main

import (
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"buct-course-local/worker/internal/course"
	"buct-course-local/worker/internal/courseflow"
	"buct-course-local/worker/internal/portable"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type companion struct {
	app                      fyne.App
	win                      fyne.Window
	store                    *portable.Store
	mobile                   bool
	status, accountStatus    *widget.Label
	tasks, courses, schedule *fyne.Container
	account                  portable.Account
	cookies                  []course.Cookie
	busy                     bool
	taskKind, urgency        string
	includeDone              bool
	week                     time.Time
	nextSync                 time.Time
	nextSessionCheck         time.Time
	stopOnce                 sync.Once
	stopped                  chan struct{}
}

func newCompanion(a fyne.App, w fyne.Window, s *portable.Store, mobile bool) *companion {
	return &companion{app: a, win: w, store: s, mobile: mobile, status: wrap(""), accountStatus: wrap(""), tasks: container.NewVBox(),
		courses: container.NewVBox(), schedule: container.NewVBox(), taskKind: "全部", urgency: "全部", stopped: make(chan struct{})}
}

func wrap(text string) *widget.Label {
	label := widget.NewLabel(text)
	label.Wrapping = fyne.TextWrapWord
	return label
}

func scroll(children ...fyne.CanvasObject) *container.Scroll {
	return container.NewVScroll(container.NewVBox(children...))
}

func (c *companion) content() fyne.CanvasObject {
	board := container.NewTabItemWithIcon("待办", theme.CheckButtonIcon(), c.buildTasks())
	classes := container.NewTabItemWithIcon("课程", theme.ListIcon(), c.buildCourses())
	schedule := container.NewTabItemWithIcon("课表", theme.CalendarIcon(), c.buildSchedule())
	settings := container.NewTabItemWithIcon("设置", theme.SettingsIcon(), c.buildAccount())
	tabs := container.NewAppTabs(board, classes, schedule, settings)
	if c.mobile {
		tabs.SetTabLocation(container.TabLocationBottom)
	}
	tabs.OnSelected = func(tab *container.TabItem) {
		c.setStatus("")
		switch tab {
		case board:
			c.renderTasks()
		case classes:
			c.renderCourses()
		case schedule:
			c.renderSchedule()
		case settings:
			c.updateAccount()
		}
	}
	c.renderTasks()
	c.renderCourses()
	c.renderSchedule()
	c.updateAccount()
	return container.NewBorder(nil, c.status, nil, nil, tabs)
}

func (c *companion) setStatus(value string) {
	c.status.SetText(value)
	if value == "" {
		c.status.Hide()
	} else {
		c.status.Show()
	}
}

func (c *companion) start() {
	c.app.Lifecycle().SetOnEnteredForeground(func() { fyne.Do(c.tick) })
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-c.stopped:
				return
			case <-ticker.C:
				fyne.Do(c.tick)
			}
		}
	}()
}

func (c *companion) tick() {
	select {
	case <-c.stopped:
		return
	default:
	}
	c.checkNotices()
	if c.account.StudentID == "" || c.busy {
		return
	}
	now := time.Now()
	if !now.Before(c.nextSync) {
		c.syncTasks()
	} else if !now.Before(c.nextSessionCheck) {
		c.keepSession()
	}
}

func (c *companion) stop() { c.stopOnce.Do(func() { close(c.stopped) }) }

func (c *companion) checkNotices() {
	if !notificationsEnabled() {
		return
	}
	now := time.Now()
	tasks, taskErr := c.store.Tasks()
	state, scheduleErr := c.store.Schedule()
	notifyTasks := c.store.Setting("notifications", 1) == 1
	var planned map[string]bool
	if c.mobile && taskErr == nil && scheduleErr == nil {
		planned = c.reconcileMobileAlarms(now, tasks, state, notifyTasks)
	}
	if notifyTasks && taskErr == nil {
		for _, item := range tasks {
			remaining := item.Deadline - now.Unix()
			if item.Completed || remaining <= 0 || remaining > 86400 {
				continue
			}
			hours := 24
			if remaining <= 3600 {
				hours = 1
			} else if remaining <= 12*3600 {
				hours = 12
			}
			if fresh, err := c.store.MarkTaskNotice(item, hours); err == nil && fresh {
				if !planned[taskAlarmKey(item, hours)] {
					c.app.SendNotification(fyne.NewNotification(fmt.Sprintf("%d 小时内截止", hours), item.Title))
				}
			}
		}
	}
	if scheduleErr == nil {
		for _, notice := range courseflow.UpcomingNotices(state, now) {
			if notice.At <= now.Unix() {
				if fresh, err := c.store.MarkScheduleNotice(notice.Key, now.Unix()); err == nil && fresh {
					if !planned["schedule:"+notice.Key] {
						c.app.SendNotification(fyne.NewNotification(notice.Title, notice.Body))
					}
				}
			}
		}
	}
}

func (c *companion) run(request course.Request, account portable.Account, message string, done func(course.Result)) {
	if c.busy {
		c.setStatus("有同步正在进行。")
		return
	}
	c.busy = true
	if message != "" {
		c.setStatus(message)
	}
	request.StudentID = account.StudentID
	request.Password = account.Password
	if request.Mode != "login" {
		request.Cookies = append([]course.Cookie(nil), c.cookies...)
	}
	go func() {
		result := course.Run(request)
		select {
		case <-c.stopped:
			return
		default:
		}
		fyne.Do(func() {
			select {
			case <-c.stopped:
				return
			default:
			}
			c.busy = false
			if result.NewSession {
				c.cookies = nil
			}
			if result.NeedsLogin {
				c.cookies = nil
				c.nextSessionCheck = time.Now().Add(25 * time.Minute)
			} else {
				c.cookies = mergeCookies(c.cookies, result.Cookies)
			}
			if result.SessionCheckAt > 0 {
				c.nextSessionCheck = time.Unix(result.SessionCheckAt, 0)
			}
			done(result)
		})
	}()
}

func (c *companion) keepSession() {
	c.nextSessionCheck = time.Now().Add(2 * time.Minute)
	c.run(course.Request{Mode: "session"}, c.account, "", func(result course.Result) {
		if result.NeedsLogin {
			c.setStatus(safeError(result))
		}
	})
}

func mergeCookies(saved, updates []course.Cookie) []course.Cookie {
	all := make(map[[3]string]course.Cookie)
	now := time.Now().Unix()
	for _, items := range [][]course.Cookie{saved, updates} {
		for _, item := range items {
			key := [3]string{item.Name, item.Domain, item.Path}
			if item.Expires > 0 && item.Expires <= now {
				delete(all, key)
			} else {
				all[key] = item
			}
		}
	}
	var result []course.Cookie
	for _, cookie := range all {
		result = append(result, cookie)
	}
	return result
}

func (c *companion) openSchool(raw string) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Host != "course.buct.edu.cn" {
		c.setStatus("学校链接无效。")
		return
	}
	if err := c.app.OpenURL(parsed); err != nil {
		c.setStatus("无法打开系统浏览器。")
	}
}

func (c *companion) showForm(title string, items []*widget.FormItem, onSave func()) {
	width, height := float32(430), float32(400)
	if c.mobile {
		size := c.win.Canvas().Size()
		width = max(240, min(width, size.Width-34))
		height = max(160, min(height, size.Height-165))
	}
	fields := container.NewVScroll(widget.NewForm(items...))
	fields.SetMinSize(fyne.NewSize(width-30, min(height, float32(55*len(items)))))
	dialog.ShowCustomConfirm(title, "保存", "取消", fields, func(ok bool) {
		if ok {
			onSave()
		}
	}, c.win)
}

func safeError(result course.Result) string {
	if result.Error != "" {
		return result.Error
	}
	if result.NeedsLogin {
		return "学校会话已过期，请重新连接账号。"
	}
	return ""
}

func titleDate(raw string) string { return strings.ReplaceAll(raw, "T", " ") }
