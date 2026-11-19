package main

import (
	"fmt"
	"strings"

	"buct-course-local/worker/internal/course"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func (c *companion) buildCourses() fyne.CanvasObject {
	return container.NewBorder(container.NewGridWithColumns(2,
		widget.NewButtonWithIcon("刷新课程目录", theme.ViewRefreshIcon(), c.refreshCourses),
		widget.NewButton("打开北化在线", func() { c.openSchool("https://course.buct.edu.cn/meol/index.do") })), nil, nil, nil, container.NewVScroll(c.courses))
}

func (c *companion) refreshCourses() {
	c.withAccount(func() {
		c.run(course.Request{Mode: "catalog"}, c.account, "正在刷新课程目录…", func(result course.Result) {
			if message := safeError(result); message != "" {
				c.setStatus(message)
				return
			}
			if result.Warning != "" {
				var old []course.CatalogCourse
				_ = c.store.ReadCache("catalog", &old)
				seen := map[string]bool{}
				var merged []course.CatalogCourse
				for _, item := range append(old, result.Courses...) {
					if !seen[item.ID] {
						seen[item.ID] = true
						merged = append(merged, item)
					}
				}
				result.Courses = merged
			}
			if c.store.Cache("catalog", result.Courses) != nil {
				c.setStatus("课程目录无法保存。")
				return
			}
			c.renderCourses()
			if result.Warning != "" {
				c.setStatus(result.Warning)
			} else {
				c.setStatus(fmt.Sprintf("课程目录已刷新 · %d 门", len(result.Courses)))
			}
		})
	})
}

func (c *companion) renderCourses() {
	var items []course.CatalogCourse
	_ = c.store.ReadCache("catalog", &items)
	var cards []fyne.CanvasObject
	for _, row := range items {
		item := row
		meta := strings.Trim(strings.Join([]string{item.Teacher, item.Semester}, " · "), " ·")
		buttons := container.NewGridWithColumns(2,
			widget.NewButton("作业", func() { c.loadCourseItems(item, "course-homework", false) }),
			widget.NewButton("测试", func() { c.loadCourseItems(item, "course-tests", false) }))
		cards = append(cards, widget.NewCard(item.Name, meta, buttons))
	}
	if len(cards) == 0 {
		cards = append(cards, widget.NewCard("暂无课程目录", "", wrap("连接学校账号后，点击刷新课程目录。")))
	}
	c.courses.Objects = cards
	c.courses.Refresh()
}

func (c *companion) loadCourseItems(item course.CatalogCourse, mode string, force bool) {
	key := mode + ":" + item.ID
	if !force {
		var cached []course.CatalogEntry
		if err := c.store.ReadCache(key, &cached); err == nil {
			c.showCourseItems(item, mode, cached)
			return
		}
	}
	c.withAccount(func() {
		c.run(course.Request{Mode: mode, CourseID: item.ID}, c.account, "正在读取课程页面…", func(result course.Result) {
			if message := safeError(result); message != "" {
				c.setStatus(message)
				return
			}
			if c.store.Cache(key, result.Entries) != nil {
				c.setStatus("无法保存课程列表。")
				return
			}
			c.setStatus("")
			c.showCourseItems(item, mode, result.Entries)
		})
	})
}

func (c *companion) showCourseItems(item course.CatalogCourse, mode string, entries []course.CatalogEntry) {
	var d dialog.Dialog
	controls := []fyne.CanvasObject{widget.NewButton("刷新此列表", func() { d.Hide(); c.loadCourseItems(item, mode, true) })}
	for _, row := range entries {
		entry := row
		meta := strings.Trim(strings.Join([]string{entry.Status, titleDate(entry.Deadline), entry.Score}, " · "), " ·")
		var actions []fyne.CanvasObject
		if entry.ItemID != "" {
			actions = append(actions, widget.NewButton("详情", func() { d.Hide(); c.lookupDetail(entry.Kind, item.ID, entry.ItemID, entry.Title) }))
		}
		if entry.URL != "" {
			actions = append(actions, widget.NewButton("打开页面", func() { c.openSchool(entry.URL) }))
		}
		if entry.ResultURL != "" {
			actions = append(actions, widget.NewButton("查看结果", func() { c.openSchool(entry.ResultURL) }))
		}
		controls = append(controls, widget.NewCard(entry.Title, meta, container.NewHBox(actions...)))
	}
	if len(entries) == 0 {
		controls = append(controls, wrap("暂无记录。"))
	}
	name := "作业"
	if mode == "course-tests" {
		name = "测试"
	}
	d = dialog.NewCustom(item.Name+" · "+name, "关闭", scroll(controls...), c.win)
	d.Resize(fyne.NewSize(470, 440))
	d.Show()
}

func (c *companion) lookupDetail(kind, id, itemID, title string) {
	mode := kind + "-detail"
	key := mode + ":" + id + ":" + itemID
	var stored course.ItemDetail
	if c.store.ReadCache(key, &stored) == nil {
		c.showSchoolDetail(title, &stored)
		return
	}
	c.withAccount(func() {
		c.run(course.Request{Mode: mode, CourseID: id, ItemID: itemID}, c.account, "正在读取详情…", func(result course.Result) {
			if message := safeError(result); message != "" {
				c.setStatus(message)
				return
			}
			if result.Detail == nil {
				c.setStatus("平台没有返回详情。")
				return
			}
			_ = c.store.Cache(key, result.Detail)
			c.setStatus("")
			c.showSchoolDetail(title, result.Detail)
		})
	})
}

func (c *companion) showSchoolDetail(title string, detail *course.ItemDetail) {
	if detail.Title != "" {
		title = detail.Title
	}
	var content []fyne.CanvasObject
	for _, text := range []string{detail.Text, detail.Deadline, detail.Score, detail.Duration, detail.Questions} {
		if strings.TrimSpace(text) != "" {
			content = append(content, wrap(text))
		}
	}
	for _, file := range detail.Attachments {
		attachment := file
		content = append(content, widget.NewButtonWithIcon(attachment.Name, theme.DownloadIcon(), func() { c.openSchool(attachment.URL) }))
	}
	if len(content) == 0 {
		content = append(content, wrap("这个项目没有可预览的正文或附件。"))
	}
	d := dialog.NewCustom(title, "关闭", scroll(content...), c.win)
	d.Resize(fyne.NewSize(430, 380))
	d.Show()
}
