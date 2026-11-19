package course

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	scraper "buct-course-local/worker/internal/remindscraper"
	buct "github.com/Ling0727-ai/go-buct-course/client"
	"github.com/Ling0727-ai/go-buct-course/course_utils"
	"github.com/Ling0727-ai/go-buct-course/exam"
)

type Task struct {
	ID       string        `json:"id"`
	Kind     string        `json:"kind"`
	Subject  string        `json:"subject"`
	Title    string        `json:"title"`
	Details  string        `json:"details"`
	URL      string        `json:"url"`
	Deadline int64         `json:"deadline"`
	Resource *TaskResource `json:"resource,omitempty"`
}

type TaskResource struct {
	CourseID string `json:"courseId"`
	ItemID   string `json:"itemId"`
	URL      string `json:"url"`
}

func pendingTasks(httpClient *http.Client, transport *courseTransport) ([]Task, []string, string, error) {
	client := &buct.BUCTClient{
		HTTPClient: httpClient,
		CourseMgr:  course_utils.New(httpClient),
		ExamMgr:    exam.New(httpClient),
	}
	var tasks []Task
	var incomplete, warnings []string
	for _, source := range []struct {
		kind, label string
		fetch       func(*buct.BUCTClient) ([]scraper.TaskInfo, error)
	}{
		{"homework", "作业", scraper.Homework},
		{"test", "测试", scraper.Tests},
	} {
		transport.pageErr = nil
		items, err := source.fetch(client)
		if cause := transport.failure(err); cause != nil {
			if errors.Is(cause, errSession) {
				return nil, nil, "", cause
			}
			// The original homework scraper may skip a course whose request fails.
			// Keep that category's cached records while applying the returned tasks.
			incomplete = append(incomplete, source.kind)
			warnings = append(warnings, fmt.Sprintf("部分%s读取失败（%s）", source.label, failureReason(cause)))
		}
		for _, item := range items {
			tasks = append(tasks, toTask(item))
		}
	}
	return tasks, incomplete, strings.Join(warnings, "；"), nil
}

func toTask(item scraper.TaskInfo) Task {
	title := strings.TrimPrefix(item.Title, item.Subject+" - ")
	identity := item.PlatformID
	if identity == "" {
		identity = title
	}
	var deadline int64
	if parsed, err := time.ParseInLocation("2006-01-02 15:04:05", item.Deadline, time.Local); err == nil {
		deadline = parsed.Unix()
	}
	return Task{
		ID:   taskID(item.Type, item.CourseID, identity),
		Kind: item.Type, Subject: item.Subject, Title: title, Details: item.Details, URL: item.URL, Deadline: deadline,
		Resource: &TaskResource{CourseID: item.CourseID, ItemID: item.PlatformID, URL: sameCourseURL(base, item.DetailURL)},
	}
}

func taskID(kind, courseID, identity string) string {
	return fmt.Sprintf("%s:%x", kind, sha256.Sum256([]byte(courseID+"\x00"+identity)))
}
