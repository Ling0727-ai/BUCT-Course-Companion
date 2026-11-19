package course

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type testRoundTripper func(*http.Request) (*http.Response, error)

func (f testRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestSyncUsesWebViewCookieAndReturnsRotatedSession(t *testing.T) {
	const browserUA = "Mozilla/5.0 WebView2 Test"
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	requests := 0
	http.DefaultTransport = testRoundTripper(func(req *http.Request) (*http.Response, error) {
		requests++
		if req.URL.String() != base+reminderPath {
			t.Fatalf("unexpected URL: %s", req.URL)
		}
		want := "webview-session"
		if requests == 3 {
			want = "rotated"
		}
		if c, err := req.Cookie("JSESSIONID"); err != nil || c.Value != want {
			t.Fatal("WebView session was not sent to the course platform")
		}
		if req.UserAgent() != browserUA {
			t.Fatalf("request used UA %q", req.UserAgent())
		}
		response := responsePage(req, `<html><body>暂无作业或测试</body></html>`)
		if requests == 2 {
			response.Header.Add("Set-Cookie", "JSESSIONID=rotated; Path=/meol; Secure; HttpOnly; SameSite=Lax")
		}
		return response, nil
	})
	result := Run(Request{Mode: "sync", Cookies: []Cookie{courseCookie()}, UserAgent: browserUA})
	if result.NeedsLogin || result.Error != "" || len(result.Tasks) != 0 || requests != 3 {
		t.Fatalf("expected a successful empty snapshot, got %+v", result)
	}
	if len(result.Cookies) != 1 || result.Cookies[0] != (Cookie{
		Name: "JSESSIONID", Value: "rotated", Domain: host, Path: "/meol", Secure: true, HTTPOnly: true, SameSite: "Lax",
	}) {
		t.Fatalf("incorrect rotating session attributes: %+v", result.Cookies)
	}
}

func TestLoggedOutPageDoesNotBecomeEmptySnapshot(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = testRoundTripper(func(req *http.Request) (*http.Response, error) {
		return responsePage(req, `<link href="/meol/styles/main/css/error.css" rel="stylesheet">`+string(loggedOutGBK)), nil
	})
	result := Run(Request{Mode: "sync", Cookies: []Cookie{courseCookie()}})
	if !result.NeedsLogin || result.Tasks != nil {
		t.Fatalf("expected a login prompt, got %+v", result)
	}
}

func TestRemindScraperReadsHomeworkAndTestsWithEmbeddedErrorStylesheet(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = testRoundTripper(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case reminderPath:
			return responsePage(req, `<a onclick="go('item?lid=111&amp;t=hw')">Course A</a>
				<a onclick="go('item?lid=222&amp;t=hw')">Course B</a>
				<a onclick="go('item?lid=333&amp;t=test')">Course C</a>`), nil
		case "/meol/jpk/course/layout/newpage/index.jsp":
			if req.URL.Query().Get("courseId") == "111" {
				return statusPage(req, http.StatusBadGateway), nil
			}
			return responsePage(req, `<link href="/meol/styles/main/css/error.css" rel="stylesheet">
				<table class="valuelist"><tr>
				<td><a href="/meol/common/hw/student/hwtask.view.jsp?hwtid=123">Assignment</a></td>
				<td></td><td></td><td>Teacher</td>
				<td><a class="enter" href="/meol/common/hw/student/hwtask.enter.jsp?hwtid=123">Submit</a></td>
				</tr></table>`), nil
		case "/meol/common/hw/student/hwtask.view.jsp":
			return responsePage(req, `<div id="body"><p>Read the text.</p></div>`), nil
		case "/meol/common/question/test/student/list.jsp":
			return responsePage(req, `<table class="valuelist"><tr><th>Tests</th></tr><tr>
				<td>Quiz</td><td></td><td>2099-12-01 12:30:00</td><td></td><td></td>
				<td><a onclick="gotostart('456')">Start</a></td><td></td><td></td>
				</tr></table>`), nil
		default:
			t.Fatalf("unexpected path: %s", req.URL.Path)
			return nil, nil
		}
	})
	result := Run(Request{Mode: "sync", Cookies: []Cookie{courseCookie()}})
	if result.Error != "" || result.NeedsLogin || len(result.Tasks) != 2 || result.Tasks[0].Details != "Read the text." ||
		result.Tasks[1].Title != "Quiz" || result.Tasks[1].Subject != "Course C" || result.Tasks[1].Deadline == 0 {
		t.Fatalf("expected assignment and quiz from the original scraper, got %+v", result)
	}
	if len(result.IncompleteKinds) != 1 || result.IncompleteKinds[0] != "homework" ||
		!strings.Contains(result.Warning, "HTTP 502") {
		t.Fatalf("expected the failed course to preserve cached homework, got %+v", result)
	}
	if !strings.HasSuffix(result.Tasks[0].URL, "index.jsp?courseId=222") {
		t.Fatalf("unexpected original course URL: %s", result.Tasks[0].URL)
	}
}

func TestSessionExpiryDuringSyncIsPreservedThroughParser(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = testRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == reminderPath {
			return responsePage(req, `<a onclick="go('item?lid=111&amp;t=hw')">Course A</a>`), nil
		}
		response := statusPage(req, http.StatusFound)
		response.Header.Set("Location", "https://portal.buct.edu.cn/login")
		return response, nil
	})
	result := Run(Request{Mode: "sync", Cookies: []Cookie{courseCookie()}})
	if !result.NeedsLogin || result.Tasks != nil || result.Warning != "" {
		t.Fatalf("expected login without a partial snapshot, got %+v", result)
	}
}

func TestAuthenticationRedirectUsesCurrentStudentHome(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = testRoundTripper(func(req *http.Request) (*http.Response, error) {
		response := statusPage(req, http.StatusFound)
		response.Header.Set("Location", legacyHomePath)
		return response, nil
	})
	req, _ := http.NewRequest(http.MethodGet, base+"/meol/homepage/common/sso_login.jsp", nil)
	response, err := newTransport("").RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if got := response.Header.Get("Location"); got != base+homePath {
		t.Fatalf("redirect target is %q", got)
	}
}

func statusPage(req *http.Request, code int) *http.Response {
	response := responsePage(req, "")
	response.StatusCode = code
	return response
}

func TestKeepaliveRespectsCookieExpiryAndRotation(t *testing.T) {
	const now = 100_000
	old := courseCookie()
	if got := sessionCheckAt(now, []Cookie{old}, nil); got != now+25*60 {
		t.Fatalf("session-cookie interval: %d", got)
	}
	old.Expires = now + 8*60
	if got := sessionCheckAt(now, []Cookie{old}, nil); got != now+6*60 {
		t.Fatalf("should run before cookie expiry: %d", got)
	}
	rotated := old
	rotated.Expires = now + 3600
	if got := sessionCheckAt(now, []Cookie{old}, []Cookie{rotated}); got != now+25*60 {
		t.Fatalf("old cookie expiry was not replaced: %d", got)
	}
}

func TestKeepaliveGetsRotatedCookieWithoutScrapingTasks(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	requests := 0
	http.DefaultTransport = testRoundTripper(func(req *http.Request) (*http.Response, error) {
		requests++
		if req.URL.Path != reminderPath || req.Method != http.MethodGet {
			t.Fatalf("unexpected keepalive request: %s %s", req.Method, req.URL)
		}
		if c, err := req.Cookie("JSESSIONID"); err != nil || c.Value != "webview-session" {
			t.Fatal("keepalive did not reuse the saved session")
		}
		response := responsePage(req, "<html>signed in</html>")
		response.Header.Add("Set-Cookie", "JSESSIONID=renewed; Path=/meol; Secure; HttpOnly")
		return response, nil
	})
	result := Run(Request{Mode: "session", Cookies: []Cookie{courseCookie()}})
	if requests != 1 || result.Tasks != nil || result.NeedsLogin || result.Error != "" || result.SessionCheckAt == 0 ||
		len(result.Cookies) != 1 || result.Cookies[0].Value != "renewed" {
		t.Fatalf("unexpected keepalive result: %+v", result)
	}
}

func responsePage(req *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK, Request: req,
		Header: http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
		Body:   io.NopCloser(strings.NewReader(body)),
	}
}

func courseCookie() Cookie {
	return Cookie{Name: "JSESSIONID", Value: "webview-session", Domain: host, Path: "/meol", Secure: true}
}
