package course

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func useCourseFixture(t *testing.T, request func(*http.Request) *http.Response) {
	t.Helper()
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = testRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == reminderPath {
			return responsePage(req, "<html>Course reminders</html>"), nil
		}
		return request(req), nil
	})
}

func TestCatalogUsesCurrentCourseListAndDeduplicatesCourses(t *testing.T) {
	useCourseFixture(t, func(req *http.Request) *http.Response {
		if req.URL.Path != catalogPath || req.Header.Get("Referer") != base+homePath {
			t.Fatalf("unexpected catalog request %s", req.URL)
		}
		return responsePage(req, `<table><tr>
		<td><a href="/meol/jpk/course/layout/newpage/index.jsp?courseId=123">Calculus</a></td>
		<td>Teacher</td><td>2026 autumn</td><td>Active</td></tr></table>
		<a href="/meol/jpk/course/layout/newpage/index.jsp?courseId=123">duplicate</a>
		<a href="https://example.invalid/meol/jpk/course/layout/newpage/index.jsp?courseId=234">external</a>`)
	})
	result := Run(Request{Mode: "catalog", Cookies: []Cookie{courseCookie()}})
	if result.Error != "" || result.Warning != "" || len(result.Courses) != 1 || result.Courses[0].Teacher != "Teacher" {
		t.Fatalf("unexpected catalog: %+v", result)
	}
}

func TestHomeworkDetailEntersCourseAndParsesTrustedAttachments(t *testing.T) {
	entered, columnOpened := false, false
	useCourseFixture(t, func(req *http.Request) *http.Response {
		switch req.URL.Path {
		case coursePath:
			entered = req.URL.Query().Get("courseId") == "123"
			return responsePage(req, "<html>Course context</html>")
		case "/meol/common/hw/student/hwtask.jsp":
			columnOpened = entered && req.Header.Get("Referer") == courseURL("123")
			return responsePage(req, "<html>Homework column</html>")
		case homeworkPath:
			if !columnOpened || req.URL.Query().Get("hwtid") != "456" {
				t.Fatal("homework detail did not use the correct course context")
			}
			return responsePage(req, `<h2>Homework 1</h2>
			<input type="hidden" name="content" value="&lt;p&gt;Read chapter one.&lt;/p&gt;">
			<a href="/meol/download/worksheet.pdf">Worksheet</a>
			<a href="/meol/download/worksheet.pdf">Duplicate</a>
			<a href="https://example.invalid/download/unsafe">External</a>`)
		default:
			t.Fatalf("unexpected request %s", req.URL)
			return nil
		}
	})
	result := Run(Request{Mode: "homework-detail", CourseID: "123", ItemID: "456", Cookies: []Cookie{courseCookie()}})
	if result.Error != "" || result.Detail == nil || result.Detail.Text != "Read chapter one." || len(result.Detail.Attachments) != 1 ||
		result.Detail.Attachments[0].URL != base+"/meol/download/worksheet.pdf" {
		t.Fatalf("unexpected homework detail: %+v", result)
	}
}

func TestCourseTestsReuseParserAcrossPagesAndExposeResult(t *testing.T) {
	useCourseFixture(t, func(req *http.Request) *http.Response {
		query := req.URL.Query()
		if req.URL.Path != testsPath || query.Get("cateId") != "123" || query.Get("pagingNumberPer") != "30" {
			t.Fatalf("unexpected test request %s", req.URL)
		}
		var entries strings.Builder
		entries.WriteString(`<table class="valuelist"><tr><th>Tests</th></tr>`)
		switch query.Get("pagingPage") {
		case "1":
			for id := 1; id <= 30; id++ {
				fmt.Fprintf(&entries, `<tr><td>Quiz %d</td><td></td><td></td><td></td><td></td>
				<td><a onclick="gotostart('%d')">Start</a></td><td></td><td></td></tr>`, id, id)
			}
		case "2":
			entries.WriteString(`<tr><td>Previous quiz</td><td></td><td></td><td></td><td></td>
			<td></td><td></td><td><a href="result.jsp?testId=31">Result</a></td></tr>`)
		default:
			t.Fatal("pagination did not stop")
		}
		return responsePage(req, entries.String()+`</table>`)
	})
	result := Run(Request{Mode: "course-tests", CourseID: "123", Cookies: []Cookie{courseCookie()}})
	if result.Error != "" || len(result.Entries) != 31 || result.Entries[30].ItemID != "31" || result.Entries[30].Available ||
		!strings.HasSuffix(result.Entries[30].ResultURL, "/result.jsp?testId=31") {
		t.Fatalf("unexpected paginated tests: %d (%s)", len(result.Entries), result.Error)
	}
}
