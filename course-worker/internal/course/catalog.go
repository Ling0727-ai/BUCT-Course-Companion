package course

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Ling0727-ai/go-buct-course/course_utils"
	"github.com/Ling0727-ai/go-buct-course/exam"
	lid "github.com/Ling0727-ai/go-buct-course/lid_utils"
	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html/charset"
)

const catalogPath = "/meol/welcomepage/student/course_list.jsp"
const coursePath = "/meol/jpk/course/layout/newpage/index.jsp"
const homeworkPath = "/meol/common/hw/student/hwtask.view.jsp"
const testsPath = "/meol/common/question/test/student/list.jsp"

type CatalogCourse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Teacher  string `json:"teacher"`
	Semester string `json:"semester"`
}

type CatalogEntry struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	ItemID    string `json:"itemId"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Start     string `json:"start"`
	Deadline  string `json:"deadline"`
	Score     string `json:"score"`
	URL       string `json:"url"`
	ResultURL string `json:"resultUrl"`
	Available bool   `json:"available"`
}

type Attachment struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type ItemDetail struct {
	Title       string       `json:"title"`
	Text        string       `json:"text"`
	Start       string       `json:"start"`
	Deadline    string       `json:"deadline"`
	Score       string       `json:"score"`
	Publisher   string       `json:"publisher"`
	Duration    string       `json:"duration"`
	Questions   string       `json:"questions"`
	Attachments []Attachment `json:"attachments"`
}

func courseURL(id string) string { return base + coursePath + "?courseId=" + url.QueryEscape(id) }

func validID(id string) bool {
	if id == "" || len(id) > 32 || strings.Trim(id, "0") == "" {
		return false
	}
	return strings.IndexFunc(id, func(r rune) bool { return r < '0' || r > '9' }) == -1
}

func sameCourseURL(origin, raw string) string {
	if strings.TrimSpace(raw) == "" || strings.HasPrefix(raw, "#") {
		return ""
	}
	from, err := url.Parse(origin)
	if err != nil {
		return ""
	}
	target, err := from.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(target.Host, host) || target.User != nil ||
		(target.Scheme != "https" && target.Scheme != "http") {
		return ""
	}
	target.Scheme = "https"
	return target.String()
}

func readCourseData(client *http.Client, transport *courseTransport, request Request, result *Result) error {
	transport.pageErr = nil
	transport.checkTestPages = request.Mode == "course-tests" || request.Mode == "test-detail"
	if request.Mode != "catalog" && !validID(request.CourseID) {
		result.Error = "课程编号无效。"
		return nil
	}
	if (request.Mode == "homework-detail" || request.Mode == "test-detail") && !validID(request.ItemID) {
		result.Error = "任务编号无效。"
		return nil
	}
	var err error
	switch request.Mode {
	case "catalog":
		result.Courses, result.Warning, err = readCatalog(client, transport)
	case "course-homework":
		result.Entries, err = readHomeworkList(client, request.CourseID)
	case "course-tests":
		result.Entries, err = readTestList(client, request.CourseID)
	case "homework-detail":
		result.Detail, err = readHomeworkDetail(client, transport, request.CourseID, request.ItemID)
	case "test-detail":
		var detail *exam.TestInfo
		detail, err = exam.New(client).GetTestDetail(request.ItemID)
		if err == nil {
			if detail.Title == "" && detail.Description == "" && detail.StartTime == "" && detail.EndTime == "" &&
				detail.Duration == "" && detail.TotalScore == "" && detail.QuestionCount == "" {
				return transport.failure(errCoursePage)
			}
			result.Detail = &ItemDetail{Title: detail.Title, Text: detail.Description, Start: detail.StartTime,
				Deadline: detail.EndTime, Duration: detail.Duration, Score: detail.TotalScore, Questions: detail.QuestionCount}
		}
	}
	return transport.failure(err)
}

func readCatalog(client *http.Client, transport *courseTransport) ([]CatalogCourse, string, error) {
	doc, docURL, err := courseDocument(client, base+catalogPath, base+homePath)
	if errors.Is(err, errSession) {
		return nil, "", err
	}
	if err == nil {
		entries := catalogLinks(doc, docURL)
		if len(entries) > 0 || strings.Contains(doc.Text(), "暂无课程") || strings.Contains(doc.Text(), "没有课程") {
			return entries, "", nil
		}
	}
	// Older MEOL deployments exposed the list directly on the student homepage.
	transport.pageErr = nil
	manager := lid.New(client)
	legacy, err := manager.GetAllCourseLids()
	if cause := transport.failure(err); errors.Is(cause, errSession) {
		return nil, "", cause
	} else if cause == nil && len(legacy) > 0 {
		return catalogFromCourses(legacy), "", nil
	}
	transport.pageErr = nil
	pending, err := manager.GetPendingTasks()
	if err != nil {
		return nil, "", err
	}
	return catalogFromCourses(append(pending.Homework, pending.Tests...)), "完整课程目录暂不可用，当前显示从待办发现的课程。", nil
}

func catalogLinks(doc *goquery.Document, origin string) []CatalogCourse {
	var result []CatalogCourse
	seen := make(map[string]bool)
	doc.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		raw, _ := link.Attr("href")
		resolved := sameCourseURL(origin, raw)
		target, err := url.Parse(resolved)
		if err != nil || target.Path != coursePath {
			return
		}
		id, name := target.Query().Get("courseId"), strings.TrimSpace(link.Text())
		if !validID(id) || name == "" || seen[id] {
			return
		}
		seen[id] = true
		cells := link.Closest("tr").ChildrenFiltered("td")
		course := CatalogCourse{ID: id, Name: name, URL: resolved}
		if cells.Length() >= 4 && cells.Eq(0).Find("a").Length() > 0 {
			course.Teacher = strings.TrimSpace(cells.Eq(1).Text())
			course.Semester = strings.TrimSpace(cells.Eq(2).Text())
		}
		result = append(result, course)
	})
	return result
}

func catalogFromCourses(items []*lid.CourseInfo) []CatalogCourse {
	var courses []CatalogCourse
	seen := make(map[string]bool)
	for _, item := range items {
		if validID(item.LID) && !seen[item.LID] {
			seen[item.LID] = true
			courses = append(courses, CatalogCourse{ID: item.LID, Name: item.CourseName, URL: courseURL(item.LID), Teacher: item.Teacher, Semester: item.Semester})
		}
	}
	return courses
}

func readHomeworkList(client *http.Client, courseID string) ([]CatalogEntry, error) {
	course, err := course_utils.New(client).GetCourseDetails(courseID)
	if err != nil {
		return nil, err
	}
	entries := make([]CatalogEntry, 0, len(course.HomeworkList))
	for _, hw := range course.HomeworkList {
		entryURL := sameCourseURL(courseURL(courseID), hw.DetailHref)
		if entryURL == "" {
			entryURL = courseURL(courseID)
		}
		entries = append(entries, CatalogEntry{ID: taskID("homework", courseID, hw.HwTID), Kind: "homework", ItemID: hw.HwTID,
			Title: hw.Title, Status: hw.Status, Deadline: hw.Deadline, Score: hw.Score, Available: hw.CanSubmit,
			URL: entryURL, ResultURL: sameCourseURL(hw.SourceURL, hw.ResultHref)})
	}
	return entries, nil
}

func readTestList(client *http.Client, courseID string) ([]CatalogEntry, error) {
	const pageSize = 30
	const maxPages = 12
	listURL := base + testsPath + "?status=1&tagbug=client&strStyle=new03&cateId=" + url.QueryEscape(courseID)
	var entries []CatalogEntry
	seen := make(map[string]bool)
	for number := 1; number <= maxPages; number++ {
		paged := *client
		paged.Transport = testPageTransport{next: client.Transport, page: number}
		tests, err := exam.New(&paged).GetTestList(courseID)
		if err != nil {
			return nil, err
		}
		before := len(entries)
		for _, test := range tests.List {
			resultURL := sameCourseURL(listURL, test.ResultHref)
			itemID := test.TestID
			if !validID(itemID) && resultURL != "" {
				if parsed, err := url.Parse(resultURL); err == nil {
					itemID = parsed.Query().Get("testId")
				}
			}
			identity := itemID
			if !validID(identity) {
				identity = test.Title
				itemID = ""
			}
			id := taskID("test", courseID, identity)
			if seen[id] {
				continue
			}
			seen[id] = true
			entries = append(entries, CatalogEntry{ID: id, Kind: "test", ItemID: itemID,
				Title: test.Title, Status: test.Status, Start: test.StartTime, Deadline: test.EndTime, Available: test.CanStart,
				URL: listURL, ResultURL: resultURL})
		}
		if len(tests.List) < pageSize || len(entries) == before {
			return entries, nil
		}
	}
	return nil, errors.New("测试列表分页超出限制")
}

// Reuse the upstream single-page parser for each page of the course test list.
type testPageTransport struct {
	next http.RoundTripper
	page int
}

func (t testPageTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Host == host && request.URL.Path == testsPath {
		request = request.Clone(request.Context())
		query := request.URL.Query()
		query.Set("pagingPage", strconv.Itoa(t.page))
		query.Set("pagingNumberPer", "30")
		request.URL.RawQuery = query.Encode()
	}
	return t.next.RoundTrip(request)
}

func courseDocument(client *http.Client, raw, referer string) (*goquery.Document, string, error) {
	request, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		return nil, "", err
	}
	request.Header.Set("Referer", referer)
	response, err := client.Do(request)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, "", pageStatus(response.StatusCode)
	}
	const limit = 4 << 20
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || len(body) > limit {
		return nil, "", errCoursePage
	}
	if bytes.Contains(body, []byte(loggedOutText)) || bytes.Contains(body, loggedOutGBK) {
		return nil, "", errSession
	}
	reader, err := charset.NewReader(bytes.NewReader(body), response.Header.Get("Content-Type"))
	if err != nil {
		return nil, "", err
	}
	doc, err := goquery.NewDocumentFromReader(reader)
	return doc, response.Request.URL.String(), err
}
