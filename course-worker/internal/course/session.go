package course

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/Ling0727-ai/go-buct-course/auth"
	"golang.org/x/text/encoding/simplifiedchinese"
)

const host = "course.buct.edu.cn"
const base = "https://" + host
const legacyHomePath = "/meol/homepage/student/index.jsp"
const homePath = "/meol/welcomepage/student/index.jsp"
const reminderPath = "/meol/welcomepage/student/interaction_reminder_v8.jsp"

var errSession = errors.New("课程平台会话已过期")
var errCoursePage = errors.New("课程平台返回错误页面")
var loggedOutText = "用户身份错误，请重新登录"
var loggedOutGBK, _ = simplifiedchinese.GBK.NewEncoder().Bytes([]byte(loggedOutText))

type Cookie struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Domain   string `json:"domain"`
	Path     string `json:"path"`
	Expires  int64  `json:"expires"`
	Secure   bool   `json:"secure"`
	HTTPOnly bool   `json:"httpOnly"`
	SameSite string `json:"sameSite"`
}

type Request struct {
	Mode      string   `json:"mode"`
	StudentID string   `json:"studentId"`
	Password  string   `json:"password"`
	Cookies   []Cookie `json:"cookies"`
	UserAgent string   `json:"userAgent"`
	CourseID  string   `json:"courseId"`
	ItemID    string   `json:"itemId"`
}

type Result struct {
	Tasks           []Task          `json:"tasks,omitempty"`
	Cookies         []Cookie        `json:"cookies,omitempty"`
	IdentityCookies []Cookie        `json:"identityCookies,omitempty"`
	IncompleteKinds []string        `json:"incompleteKinds,omitempty"`
	Warning         string          `json:"warning,omitempty"`
	NeedsLogin      bool            `json:"needsLogin,omitempty"`
	NewSession      bool            `json:"newSession,omitempty"`
	Error           string          `json:"error,omitempty"`
	Courses         []CatalogCourse `json:"courses,omitempty"`
	Entries         []CatalogEntry  `json:"entries,omitempty"`
	Detail          *ItemDetail     `json:"detail,omitempty"`
	SessionCheckAt  int64           `json:"sessionCheckAt,omitempty"`
}

// Cookie domains are limited to the course site and its parent domain.
func courseDomain(domain string) bool {
	switch strings.TrimPrefix(strings.ToLower(domain), ".") {
	case host, "buct.edu.cn":
		return true
	}
	return false
}

type courseTransport struct {
	updates        map[[3]string]Cookie
	identity       map[[3]string]Cookie
	check          bool
	pageErr        error
	userAgent      string
	checkTestPages bool
}

func (t *courseTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	secureOrigin := req.URL.Scheme == "https" && req.URL.User == nil
	identityOrigin := secureOrigin && (req.URL.Host == "portal.buct.edu.cn" || req.URL.Host == "sidportal.buct.edu.cn")
	courseOrigin := secureOrigin && req.URL.Host == host
	if t.userAgent != "" && (courseOrigin || identityOrigin) {
		req = req.Clone(req.Context())
		req.Header.Set("User-Agent", t.userAgent)
	}
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.recordFailure(err)
		return nil, err
	}
	if identityOrigin && !t.check {
		recordCookies(t.identity, req, resp, func(domain string) bool {
			return strings.TrimPrefix(domain, ".") == req.URL.Host
		})
	}
	if !courseOrigin {
		return resp, nil
	}
	recordCookies(t.updates, req, resp, courseDomain)
	return t.checkCourseResponse(req, resp)
}

func recordCookies(updates map[[3]string]Cookie, req *http.Request, resp *http.Response, allowed func(string) bool) {
	for _, c := range resp.Cookies() {
		domain := strings.ToLower(c.Domain)
		if domain == "" {
			domain = req.URL.Host
		} else {
			domain = "." + strings.TrimPrefix(domain, ".")
		}
		if !allowed(domain) {
			continue
		}
		cookiePath := c.Path
		if !strings.HasPrefix(cookiePath, "/") {
			cookiePath = path.Dir(req.URL.Path)
			if cookiePath == "." {
				cookiePath = "/"
			}
		}
		var expires int64
		if !c.Expires.IsZero() {
			expires = max(1, c.Expires.Unix())
		}
		if c.MaxAge < 0 {
			expires = 1
		} else if c.MaxAge > 0 {
			expires = time.Now().Unix() + int64(c.MaxAge)
		}
		var sameSite string
		switch c.SameSite {
		case http.SameSiteStrictMode:
			sameSite = "Strict"
		case http.SameSiteLaxMode:
			sameSite = "Lax"
		case http.SameSiteNoneMode:
			sameSite = "None"
		}
		updates[[3]string{c.Name, domain, cookiePath}] = Cookie{
			Name: c.Name, Value: c.Value, Domain: domain, Path: cookiePath,
			Expires: expires, Secure: c.Secure, HTTPOnly: c.HttpOnly, SameSite: sameSite,
		}
	}
}

func (t *courseTransport) checkCourseResponse(req *http.Request, resp *http.Response) (*http.Response, error) {
	if !t.check {
		// Older authentication redirects can still name the retired student home.
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			if destination, err := resp.Location(); err == nil && destination.Scheme == "https" &&
				destination.Host == host && destination.User == nil && destination.Path == legacyHomePath {
				resp.Header.Set("Location", base+homePath)
			}
		}
		return resp, nil
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || isLoginURL(req.URL):
		return t.reject(resp, errSession)
	case resp.StatusCode >= 400:
		t.recordFailure(pageStatus(resp.StatusCode))
		return resp, nil
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		if destination, err := resp.Location(); err == nil && isLoginURL(destination) {
			return t.reject(resp, errSession)
		}
		return resp, nil
	case resp.StatusCode != http.StatusOK || (req.URL.Path != reminderPath &&
		!(t.checkTestPages && (req.URL.Path == testsPath || req.URL.Path == "/meol/common/question/test/student/view.jsp"))):
		return resp, nil
	}
	// The logged-out MEOL page returns HTTP 200 with the error template.
	// Keep the stream intact so the upstream parsers still see the original page.
	prefix, err := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if err != nil {
		return t.reject(resp, err)
	}
	if bytes.Contains(prefix, loggedOutGBK) || bytes.Contains(prefix, []byte(loggedOutText)) {
		return t.reject(resp, errSession)
	}
	resp.Body = prefixedBody{Reader: io.MultiReader(bytes.NewReader(prefix), resp.Body), Closer: resp.Body}
	return resp, nil
}

func (t *courseTransport) reject(response *http.Response, err error) (*http.Response, error) {
	response.Body.Close()
	t.recordFailure(err)
	return nil, err
}

func (t *courseTransport) recordFailure(err error) {
	if t.pageErr == nil || errors.Is(err, errSession) {
		t.pageErr = err
	}
}

// The upstream parsers format HTTP errors with %v. Keep the cause available
// so that a failed course page and a signed-out session remain distinguishable.
func (t *courseTransport) failure(err error) error {
	if t.pageErr != nil {
		return t.pageErr
	}
	if err != nil && err.Error() == "course session expired" {
		return errSession
	}
	return err
}

type pageStatus int

func (status pageStatus) Error() string { return "HTTP " + strconv.Itoa(int(status)) }

func failureReason(err error) string {
	var status pageStatus
	switch {
	case errors.As(err, &status):
		return status.Error()
	case errors.Is(err, errCoursePage):
		return errCoursePage.Error()
	default:
		return "页面请求或解析失败"
	}
}

func isLoginURL(uri *url.URL) bool {
	return uri.Host == "portal.buct.edu.cn" || uri.Host == "sidportal.buct.edu.cn" ||
		strings.Contains(strings.ToLower(uri.Path), "login")
}

type prefixedBody struct {
	io.Reader
	io.Closer
}

func newTransport(agent string) *courseTransport {
	if strings.ContainsAny(agent, "\r\n") {
		agent = ""
	}
	return &courseTransport{updates: make(map[[3]string]Cookie), identity: make(map[[3]string]Cookie), userAgent: agent}
}

func checkRedirect(req *http.Request, history []*http.Request) error {
	if isLoginURL(req.URL) {
		return errSession
	}
	if req.URL.Scheme != "https" || req.URL.User != nil || req.URL.Host != host {
		return errCoursePage
	}
	if len(history) >= 10 {
		return errors.New("课程页面重定向次数过多")
	}
	return nil
}

func probe(client *http.Client) error {
	response, err := client.Get(base + reminderPath)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || isLoginURL(response.Request.URL) {
		return errSession
	}
	if response.StatusCode != http.StatusOK {
		return pageStatus(response.StatusCode)
	}
	return nil
}

func Run(request Request) Result {
	jar, _ := cookiejar.New(nil)
	origin := &url.URL{Scheme: "https", Host: host}
	for _, c := range request.Cookies {
		if !courseDomain(c.Domain) || (c.Expires != 0 && c.Expires <= time.Now().Unix()) {
			continue
		}
		item := &http.Cookie{Name: c.Name, Value: c.Value, Domain: c.Domain, Path: c.Path, Secure: c.Secure, HttpOnly: c.HTTPOnly}
		if c.Domain == host {
			item.Domain = ""
		}
		if c.Expires != 0 {
			item.Expires = time.Unix(c.Expires, 0)
		}
		jar.SetCookies(origin, []*http.Cookie{item})
	}
	transport := newTransport(request.UserAgent)
	transport.check = true
	client := &http.Client{Jar: jar, Timeout: 20 * time.Second, Transport: transport, CheckRedirect: checkRedirect}
	var err error
	newSession := false
	if len(request.Cookies) == 0 || request.Mode == "login" {
		err = errSession
	} else {
		err = probe(client)
	}
	if errors.Is(err, errSession) {
		if request.StudentID == "" || request.Password == "" {
			return Result{NeedsLogin: true}
		}
		login := auth.New()
		transport = newTransport(request.UserAgent)
		login.Client.Transport = transport
		if err := login.Login(request.StudentID, request.Password); err != nil {
			return Result{NeedsLogin: true, Error: "自动登录未成功，请核对凭证，或在内置页面完成学校认证。"}
		}
		transport.check = true
		client.Jar = login.Client.Jar
		client.Transport = transport
		err = probe(client)
		newSession = err == nil
	}
	if errors.Is(err, errSession) {
		return Result{NeedsLogin: true}
	}
	if err != nil {
		return Result{Error: "无法验证课程会话（" + failureReason(err) + "），已保留本地数据。"}
	}
	result := Result{NewSession: newSession}
	switch request.Mode {
	case "sync":
		result.Tasks, result.IncompleteKinds, result.Warning, err = pendingTasks(client, transport)
		if err != nil {
			if errors.Is(err, errSession) {
				return Result{NeedsLogin: true}
			}
			result.Error = "待办课程入口读取失败（" + failureReason(err) + "），已保留上次同步的数据。"
		}
	case "catalog", "course-homework", "course-tests", "homework-detail", "test-detail":
		err = readCourseData(client, transport, request, &result)
		if errors.Is(err, errSession) {
			return Result{NeedsLogin: true}
		}
		if err != nil {
			result.Error = "读取课程数据失败（" + failureReason(err) + "）。"
		}
	}
	for _, cookie := range transport.updates {
		result.Cookies = append(result.Cookies, cookie)
	}
	if newSession {
		for _, cookie := range transport.identity {
			result.IdentityCookies = append(result.IdentityCookies, cookie)
		}
	}
	previous := request.Cookies
	if newSession {
		previous = nil
	}
	result.SessionCheckAt = sessionCheckAt(time.Now().Unix(), previous, result.Cookies)
	return result
}

func sessionCheckAt(now int64, previous, updates []Cookie) int64 {
	// Keep this independent from the task refresh interval. Session cookies may
	// have no client-visible expiry; a protected request still picks up rotation.
	next := now + 25*60
	expires := make(map[[3]string]int64)
	for _, group := range [][]Cookie{previous, updates} {
		for _, cookie := range group {
			if strings.EqualFold(cookie.Name, "JSESSIONID") && courseDomain(cookie.Domain) {
				expires[[3]string{cookie.Name, cookie.Domain, cookie.Path}] = cookie.Expires
			}
		}
	}
	for _, at := range expires {
		if at > now {
			next = min(next, max(now+30, at-120))
		}
	}
	return next
}
