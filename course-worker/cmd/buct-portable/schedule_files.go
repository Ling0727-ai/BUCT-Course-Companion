package main

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"buct-course-local/worker/internal/courseflow"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"
)

func (c *companion) importSchedule() {
	d := dialog.NewFileOpen(func(file fyne.URIReadCloser, err error) {
		if err != nil {
			showError(c.win, "无法打开所选文件。")
			return
		}
		if file == nil {
			return
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, (2<<20)+1))
		if err != nil || len(data) > 2<<20 {
			showError(c.win, "课表文件读取失败，或超过 2 MB。")
			return
		}
		state, err := c.store.Schedule()
		if err != nil {
			showError(c.win, "无法读取当前课表。")
			return
		}
		replace := !strings.EqualFold(filepath.Ext(file.URI().Name()), ".csv")
		if replace {
			var imported courseflow.State
			if json.Unmarshal(data, &imported) != nil {
				showError(c.win, "课表 JSON 格式无效。")
				return
			}
			imported.Revision = state.Revision
			state = imported
		} else {
			state, err = applyCSV(string(data), state)
			if err != nil {
				showError(c.win, err.Error())
				return
			}
		}
		if err := state.Validate(); err != nil {
			showError(c.win, err.Error())
			return
		}
		message := fmt.Sprintf("导入后共 %d 门课程、%d 项考试。", len(state.Courses), len(state.Exams))
		if replace {
			message += " JSON 将替换当前课表。"
		}
		dialog.ShowConfirm("确认导入", message, func(ok bool) {
			if ok {
				c.saveSchedule(state)
			}
		}, c.win)
	}, c.win)
	d.SetFilter(storage.NewExtensionFileFilter([]string{".json", ".csv"}))
	d.Show()
}

func (c *companion) exportMenu() {
	var d dialog.Dialog
	d = dialog.NewCustom("导出课表", "取消", container.NewVBox(
		widget.NewButton("JSON 备份", func() { d.Hide(); c.exportSchedule(false) }),
		widget.NewButton("ICS 日历", func() { d.Hide(); c.exportSchedule(true) })), c.win)
	d.Show()
}

func (c *companion) exportSchedule(calendar bool) {
	state, err := c.store.Schedule()
	if err != nil {
		showError(c.win, "无法读取课表。")
		return
	}
	var data []byte
	name := "buct-schedule.json"
	if calendar {
		data, err = exportICS(state)
		name = "buct-schedule.ics"
	} else {
		data, err = json.MarshalIndent(state, "", "  ")
	}
	if err != nil {
		showError(c.win, "无法生成导出文件。")
		return
	}
	d := dialog.NewFileSave(func(file fyne.URIWriteCloser, err error) {
		if err != nil {
			showError(c.win, "无法打开保存位置。")
			return
		}
		if file == nil {
			return
		}
		_, writeErr := file.Write(data)
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			showError(c.win, "无法写入导出文件。")
			return
		}
		c.setStatus("课表已导出。")
	}, c.win)
	d.SetFileName(name)
	d.Show()
}

func applyCSV(text string, state courseflow.State) (courseflow.State, error) {
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(text, "\uFEFF")))
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		return state, fmt.Errorf("CSV 缺少表头")
	}
	column := func(names ...string) int {
		for i, key := range header {
			for _, n := range names {
				if strings.TrimSpace(key) == n {
					return i
				}
			}
		}
		return -1
	}
	start, end := column("开始时间", "start"), column("结束时间", "end")
	name, day, period := column("课程名称", "name"), column("星期", "day"), column("节次", "period")
	if start < 0 || end < 0 || name < 0 && period < 0 || name >= 0 && day < 0 {
		return state, fmt.Errorf("CSV 表头缺少课程、星期或起止时间")
	}
	location, teacher := column("地点", "location"), column("教师", "teacher")
	first, last, parity, note := column("开始周", "week_start"), column("结束周", "week_end"), column("单双周", "parity"), column("备注", "note")
	var clocks []courseflow.PeriodTime
	byID := map[string]courseflow.Course{}
	for _, item := range state.Courses {
		byID[item.ID] = item
	}
	for line := 2; ; line++ {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil || line > 1200 {
			return state, fmt.Errorf("CSV 第 %d 行格式错误或记录过多", line)
		}
		get := func(index int) string {
			if index < 0 || index >= len(row) {
				return ""
			}
			return strings.TrimSpace(row[index])
		}
		if name < 0 {
			n, e := strconv.Atoi(get(period))
			if e != nil || n != len(clocks)+1 {
				return state, fmt.Errorf("第 %d 行的作息节次不连续", line)
			}
			clocks = append(clocks, courseflow.PeriodTime{Start: get(start), End: get(end)})
			continue
		}
		d, e := strconv.Atoi(get(day))
		if e != nil {
			for i, label := range weekNames {
				if get(day) == label {
					d = i + 1
				}
			}
		}
		a, b := 1, state.Settings.Weeks
		if value := get(first); value != "" {
			a, _ = strconv.Atoi(value)
		}
		if value := get(last); value != "" {
			b, _ = strconv.Atoi(value)
		}
		p := get(parity)
		switch p {
		case "", "每周":
			p = "all"
		case "单周":
			p = "odd"
		case "双周":
			p = "even"
		}
		lesson := courseflow.Course{Name: get(name), Teacher: get(teacher), Location: get(location), Day: d, Start: get(start), End: get(end), WeekStart: a,
			WeekEnd: b, Parity: p, Note: get(note), Color: "mint"}
		key := fmt.Sprintf("%s/%d/%s/%s/%s/%d/%d/%s", lesson.Name, d, lesson.Start, lesson.End, lesson.Location, a, b, p)
		lesson.ID = fmt.Sprintf("import-%x", sha256.Sum256([]byte(key)))
		if _, exists := byID[lesson.ID]; !exists {
			state.Courses = append(state.Courses, lesson)
			byID[lesson.ID] = lesson
		}
	}
	if name < 0 {
		for i, lesson := range state.Courses {
			if lesson.PeriodStart > 0 {
				if lesson.PeriodEnd < lesson.PeriodStart || lesson.PeriodEnd > len(clocks) {
					return state, fmt.Errorf("有课程使用了导入作息表之外的节次")
				}
				state.Courses[i].Start = clocks[lesson.PeriodStart-1].Start
				state.Courses[i].End = clocks[lesson.PeriodEnd-1].End
			}
		}
		state.Settings.Periods = clocks
	}
	return state, nil
}

func exportICS(state courseflow.State) ([]byte, error) {
	encoded, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	reply := courseflow.Run(courseflow.Request{State: encoded, Export: true}, time.Now())
	if reply.Error != "" {
		return nil, fmt.Errorf("%s", reply.Error)
	}
	var body strings.Builder
	for _, line := range []string{"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//BUCT Course Companion//CN"} {
		icsLine(&body, line)
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	for _, event := range reply.Events {
		a, e1 := time.ParseInLocation("2006-01-02 15:04", event.Date+" "+event.Start, time.Local)
		b, e2 := time.ParseInLocation("2006-01-02 15:04", event.Date+" "+event.End, time.Local)
		if e1 != nil || e2 != nil {
			continue
		}
		name := event.Name
		kind := "course"
		if event.Exam {
			name = "考试 · " + name
			kind = "exam"
		}
		for _, line := range []string{"BEGIN:VEVENT", "UID:" + icsEscape(kind+"-"+event.ID+"-"+event.Date) + "@buct-course-companion",
			"DTSTAMP:" + stamp, "DTSTART:" + a.UTC().Format("20060102T150405Z"), "DTEND:" + b.UTC().Format("20060102T150405Z"),
			"SUMMARY:" + icsEscape(name), "LOCATION:" + icsEscape(event.Location), "END:VEVENT"} {
			icsLine(&body, line)
		}
	}
	icsLine(&body, "END:VCALENDAR")
	return []byte(body.String()), nil
}

func icsEscape(value string) string {
	return strings.NewReplacer("\\", "\\\\", "\r\n", "\\n", "\n", "\\n", "\r", "\\n", ",", "\\,", ";", "\\;").Replace(value)
}

func icsLine(writer *strings.Builder, line string) {
	for len(line) > 75 {
		last := 0
		for index := range line {
			if index > 75 {
				break
			}
			last = index
		}
		writer.WriteString(line[:last])
		writer.WriteString("\r\n")
		line = " " + line[last:]
	}
	writer.WriteString(line)
	writer.WriteString("\r\n")
}
