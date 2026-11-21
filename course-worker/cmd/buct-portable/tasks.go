package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"buct-course-local/worker/internal/course"
	"buct-course-local/worker/internal/portable"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func (c *companion) buildTasks() fyne.CanvasObject {
	kind := widget.NewSelect([]string{"全部", "作业", "测试", "个人待办"}, func(value string) { c.taskKind = value; c.renderTasks() })
	kind.SetSelected(c.taskKind)
	level := widget.NewSelect([]string{"全部", "紧急", "即将到期", "正常"}, func(value string) { c.urgency = value; c.renderTasks() })
	level.SetSelected(c.urgency)
	completed := widget.NewCheck("显示已完成", func(value bool) { c.includeDone = value; c.renderTasks() })
	return container.NewBorder(container.NewVBox(container.NewGridWithColumns(2,
		widget.NewButtonWithIcon("同步课程", theme.ViewRefreshIcon(), c.syncTasks),
		widget.NewButtonWithIcon("新建待办", theme.ContentAddIcon(), func() { c.editTodo(nil) })),
		container.NewGridWithColumns(2, kind, level), completed), nil, nil, nil, container.NewVScroll(c.tasks))
}

func (c *companion) syncTasks() {
	if c.busy {
		c.setStatus("同步正在进行。")
		return
	}
	c.withAccount(func() {
		c.nextSync = time.Now().Add(15 * time.Minute)
		c.run(course.Request{Mode: "sync"}, c.account, "正在同步课程…", func(result course.Result) {
			if message := safeError(result); message != "" {
				c.setStatus(message + " 本地任务已保留。")
				if result.NeedsLogin {
					c.nextSync = time.Now().Add(3 * time.Hour)
				}
				return
			}
			if err := c.store.SaveSync(result, time.Now().Unix()); err != nil {
				c.setStatus("同步结果无法保存。")
				return
			}
			if result.Warning != "" {
				c.setStatus(result.Warning)
			} else {
				c.setStatus(fmt.Sprintf("同步完成 · %d 项课程任务", len(result.Tasks)))
				c.nextSync = time.Now().Add(3 * time.Hour)
			}
			c.renderTasks()
			c.checkNotices()
		})
	})
}

func kindName(kind string) string {
	switch kind {
	case "homework":
		return "作业"
	case "test":
		return "测试"
	default:
		return "个人待办"
	}
}

func urgencyOf(item portable.Task, now int64) string {
	if item.Completed {
		return "已完成"
	}
	if item.Deadline <= 0 {
		return "正常"
	}
	if item.Deadline-now <= 86400 {
		return "紧急"
	}
	if item.Deadline-now <= 3*86400 {
		return "即将到期"
	}
	return "正常"
}

func (c *companion) renderTasks() {
	if c.dashboardVisible() {
		c.renderDashboard()
	}
	items, err := c.store.Tasks()
	if err != nil {
		c.setStatus("无法读取本地任务。")
		return
	}
	groups := map[string][]portable.Task{}
	now := time.Now().Unix()
	for _, item := range items {
		if c.taskKind != "全部" && kindName(item.Kind) != c.taskKind {
			continue
		}
		label := urgencyOf(item, now)
		if label == "已完成" && !c.includeDone || c.urgency != "全部" && label != c.urgency {
			continue
		}
		groups[label] = append(groups[label], item)
	}
	var content []fyne.CanvasObject
	if last := c.store.Setting("last_sync", 0); last > 0 {
		content = append(content, widget.NewLabel("上次完整同步 · "+time.Unix(last, 0).In(time.Local).Format("01月02日 15:04")))
	}
	shown := 0
	for _, group := range []string{"紧急", "即将到期", "正常", "已完成"} {
		if tasks := groups[group]; len(tasks) > 0 {
			content = append(content, widget.NewLabelWithStyle(fmt.Sprintf("%s · %d 项", group, len(tasks)), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
			for _, t := range tasks {
				item := t
				meta := kindName(item.Kind) + " · " + item.Subject
				if item.Deadline > 0 {
					meta += " · " + time.Unix(item.Deadline, 0).In(time.Local).Format("01/02 15:04")
				}
				content = append(content, widget.NewCard(item.Title, "", container.NewBorder(nil, nil, nil,
					widget.NewButton("详情", func() { c.taskDetail(item) }), wrap(meta))))
				shown++
			}
		}
	}
	if shown == 0 {
		content = append(content, widget.NewCard("暂无符合条件的任务", "", wrap("可同步学校课程或新建个人待办。")))
	}
	c.tasks.Objects = content
	c.tasks.Refresh()
}

func (c *companion) taskDetail(item portable.Task) {
	var d dialog.Dialog
	var actions []fyne.CanvasObject
	button := func(text string, then func()) {
		actions = append(actions, widget.NewButton(text, func() { d.Hide(); then() }))
	}
	state := "标记完成"
	if item.Completed {
		state = "恢复为待办"
	}
	button(state, func() {
		if c.store.Complete(item) != nil {
			c.setStatus("无法更新任务。")
			return
		}
		c.renderTasks()
		c.checkNotices()
	})
	if item.Kind == "todo" {
		button("编辑待办", func() { c.editTodo(&item) })
	} else {
		if item.URL != "" {
			button("在浏览器打开", func() { c.openSchool(item.URL) })
		}
		if r := item.Resource; r != nil && r.CourseID != "" && r.ItemID != "" {
			button("学校详情与附件", func() { c.lookupDetail(item.Kind, r.CourseID, r.ItemID, item.Title) })
		}
		if item.Subject != "" {
			button("屏蔽这门课", func() {
				key := "name:" + item.Subject
				if item.Resource != nil && item.Resource.CourseID != "" {
					key = "id:" + item.Resource.CourseID
				}
				c.hide(portable.Filter{Kind: "course", Key: key, Title: item.Subject})
			})
		}
	}
	button("隐藏或移入回收站", func() {
		kind := "task"
		if item.Kind == "todo" {
			kind = "todo"
		}
		c.hide(portable.Filter{Kind: kind, Key: item.ID, Title: item.Title})
	})
	text := strings.TrimSpace(item.Details)
	if text == "" {
		text = kindName(item.Kind) + " · " + item.Subject
	}
	if item.Deadline > 0 {
		text += "\n截止 · " + time.Unix(item.Deadline, 0).In(time.Local).Format("2006-01-02 15:04")
	}
	d = dialog.NewCustom(item.Title, "关闭", scroll(append([]fyne.CanvasObject{wrap(text)}, actions...)...), c.win)
	d.Resize(fyne.NewSize(430, 390))
	d.Show()
}

func (c *companion) hide(filter portable.Filter) {
	if c.store.Hide(filter) != nil {
		c.setStatus("无法隐藏记录。")
		return
	}
	c.renderTasks()
	c.checkNotices()
	c.setStatus("已隐藏，可在设置中恢复。")
}

func (c *companion) editTodo(existing *portable.Task) {
	title := widget.NewEntry()
	due := widget.NewEntry()
	due.SetPlaceHolder("YYYY-MM-DD HH:mm")
	details := widget.NewMultiLineEntry()
	details.SetMinRowsVisible(3)
	if existing != nil {
		title.SetText(existing.Title)
		details.SetText(existing.Details)
		if existing.Deadline > 0 {
			due.SetText(time.Unix(existing.Deadline, 0).In(time.Local).Format("2006-01-02 15:04"))
		}
	}
	c.showForm("个人待办", []*widget.FormItem{widget.NewFormItem("标题", title), widget.NewFormItem("截止时间", due), widget.NewFormItem("内容", details)}, func() {
		var deadline int64
		if raw := strings.TrimSpace(due.Text); raw != "" {
			value, err := time.ParseInLocation("2006-01-02 15:04", raw, time.Local)
			if err != nil {
				showError(c.win, "请使用 YYYY-MM-DD HH:mm 格式。")
				return
			}
			deadline = value.Unix()
		}
		id := newID("todo:")
		if existing != nil {
			id = existing.ID
		}
		if err := c.store.SaveTodo(course.Task{ID: id, Kind: "todo", Subject: "个人", Title: strings.TrimSpace(title.Text), Details: details.Text, Deadline: deadline}); err != nil {
			showError(c.win, "无法保存待办，请检查标题和本地数据。 ")
			return
		}
		c.renderTasks()
		c.checkNotices()
		c.setStatus("待办已保存。")
	})
}

func (c *companion) openTrash() {
	items, err := c.store.Filters()
	if err != nil {
		c.setStatus("无法读取回收站。")
		return
	}
	var controls []fyne.CanvasObject
	for _, f := range items {
		filter := f
		row := container.NewBorder(nil, nil, nil, widget.NewButton("恢复", func() {
			if c.store.Restore(filter) != nil {
				c.setStatus("无法恢复记录。")
				return
			}
			c.renderTasks()
			c.setStatus("已恢复记录。")
		}), wrap(filter.Title))
		controls = append(controls, row)
	}
	if len(controls) == 0 {
		controls = append(controls, wrap("暂无隐藏或回收站记录。"))
	}
	d := dialog.NewCustom("隐藏与回收站", "关闭", scroll(controls...), c.win)
	d.Resize(fyne.NewSize(430, 340))
	d.Show()
}

func newID(prefix string) string {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(data[:])
}
