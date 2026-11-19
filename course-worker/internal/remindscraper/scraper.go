// Package scraper contains the BUCT-course-remind task scraper.
// The extraction functions are from web/go-backend-test/scraper at commit
// 6563bba793df487c8ba46bc87e6cae83a3bfabe5. The desktop app supplies the session.
package scraper

import buct "github.com/Ling0727-ai/go-buct-course/client"

type TaskInfo struct {
	Subject    string
	Title      string
	Deadline   string
	Details    string
	URL        string
	Type       string
	CourseID   string // Preserve existing desktop task identities.
	PlatformID string
	DetailURL  string
}

func Homework(client *buct.BUCTClient) ([]TaskInfo, error) {
	return scrapeHomework(client, "local", nil)
}

func Tests(client *buct.BUCTClient) ([]TaskInfo, error) {
	return scrapeTests(client, "local", nil)
}
