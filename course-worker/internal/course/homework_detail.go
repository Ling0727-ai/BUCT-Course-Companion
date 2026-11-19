package course

import (
	"html"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

func readHomeworkDetail(client *http.Client, transport *courseTransport, courseID, itemID string) (*ItemDetail, error) {
	// Opening the course first sets the MEOL session's current course context.
	home := courseURL(courseID)
	if _, _, err := courseDocument(client, home, base+homePath); err != nil {
		return nil, err
	}
	column := base + "/meol/common/hw/student/hwtask.jsp?tagbug=client&strStyle=new03"
	// Some courses use their own homework column. The default entry may fail
	// even though the detail itself is available after entering the course.
	prior := transport.pageErr
	_, _, _ = courseDocument(client, column, home)
	transport.pageErr = prior
	doc, original, err := courseDocument(client, base+homeworkPath+"?hwtid="+url.QueryEscape(itemID),
		column)
	if err != nil {
		return nil, err
	}
	detail := parseHomeworkDetail(doc, original)
	if detail.Title == "" && detail.Text == "" && len(detail.Attachments) == 0 && detail.Deadline == "" {
		return nil, errCoursePage
	}
	return detail, nil
}

func parseHomeworkDetail(doc *goquery.Document, original string) *ItemDetail {
	item := &ItemDetail{}
	doc.Find("table.infotable tr, table.valuelist tr").Each(func(_ int, row *goquery.Selection) {
		label, value := row.ChildrenFiltered("th"), row.ChildrenFiltered("td")
		if label.Length() != 1 || value.Length() != 1 {
			return
		}
		text := strings.TrimSpace(value.Text())
		switch header := label.Text(); {
		case strings.Contains(header, "标题"):
			item.Title = text
		case strings.Contains(header, "截止"):
			item.Deadline = text
		case strings.Contains(header, "分数") || strings.Contains(header, "评分"):
			item.Score = text
		case strings.Contains(header, "发布人"):
			item.Publisher = text
		}
	})
	if item.Title == "" {
		item.Title = strings.TrimSpace(doc.Find("h1,h2,h3,.title").First().Text())
	}
	var paragraphs []string
	seen := make(map[string]bool)
	addLink := func(link *goquery.Selection) {
		href, _ := link.Attr("href")
		resolved := sameCourseURL(original, href)
		if resolved == "" || seen[resolved] {
			return
		}
		seen[resolved] = true
		label := strings.TrimSpace(link.Text())
		if label == "" {
			uri, _ := url.Parse(resolved)
			label = path.Base(uri.Path)
		}
		item.Attachments = append(item.Attachments, Attachment{Name: label, URL: resolved})
	}
	addText := func(text string) {
		if text = strings.TrimSpace(text); text != "" && (len(paragraphs) == 0 || paragraphs[len(paragraphs)-1] != text) {
			paragraphs = append(paragraphs, text)
		}
	}
	collect := func(section *goquery.Selection) {
		blocks := section.Find("p,li")
		if blocks.Length() == 0 {
			addText(section.Text())
		}
		blocks.Each(func(_ int, p *goquery.Selection) { addText(p.Text()) })
		section.Find("a[href]").Each(func(_ int, link *goquery.Selection) { addLink(link) })
	}
	doc.Find("input[type=hidden][name][value]").Each(func(_ int, input *goquery.Selection) {
		name, _ := input.Attr("name")
		if !strings.Contains(strings.ToLower(name), "content") {
			return
		}
		value, _ := input.Attr("value")
		if !strings.Contains(value, "<") {
			value = html.UnescapeString(value)
		}
		if fragment, err := goquery.NewDocumentFromReader(strings.NewReader(value)); err == nil {
			collect(fragment.Find("body"))
		}
	})
	if len(paragraphs) == 0 {
		doc.Find("div#body,div.content,div.description,td.text").Each(func(_ int, section *goquery.Selection) { collect(section) })
	}
	doc.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href, _ := link.Attr("href")
		href = strings.ToLower(href)
		for _, hint := range []string{"download", "attach", "common/script/", "upload", "file="} {
			if strings.Contains(href, hint) {
				addLink(link)
				break
			}
		}
	})
	item.Text = strings.Join(paragraphs, "\n\n")
	return item
}
