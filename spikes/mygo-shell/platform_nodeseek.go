package main

// NodeSeek 页面解析：对应 import/nodeseek.ts。按真实楼层去重，置顶回复不影响分页顺序。

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"guizhi-mygo-shell/extract"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
)

type nodeseekPage struct {
	Title     string
	Node      string
	Posts     []forumReply
	PageCount int
}

var (
	reNodeseekVerifyTitle = regexp.MustCompile(`(?i)Just a moment|请稍候|验证|challenge|signIn`)
	selNodeseekTitle      = cascadia.MustCompile("h1 .post-title-link")
	selNodeseekChallenge  = cascadia.MustCompile("#challenge-stage, #cf-wrapper, .cf-turnstile, script[src*='/orchestrate/chl_page/'], script[src*='challenges.cloudflare.com/turnstile/']")
	selNodeseekItems      = cascadia.MustCompile(".content-item[id]")
	selNodeseekArticle    = cascadia.MustCompile("article.post-content")
	selNodeseekMedia      = cascadia.MustCompile("a[href], img[src]")
	selNodeseekAuthor     = cascadia.MustCompile(".author-name")
	selNodeseekTime       = cascadia.MustCompile(".date-created time")
	selNodeseekPaging     = cascadia.MustCompile(`[aria-label="pagination"] a[href]`)
	selNodeseekCategory   = cascadia.MustCompile(".content-category a")
	nsSelTitleTag         = cascadia.MustCompile("title")
	nsReDigits            = regexp.MustCompile(`^\d+$`)
)

func nsNodeText(n *html.Node) string {
	if n == nil {
		return ""
	}
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		if x.Type == html.TextNode {
			sb.WriteString(x.Data)
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return sb.String()
}

func nsNodeAttr(n *html.Node, name string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val, true
		}
	}
	return "", false
}

func nsSetNodeAttr(n *html.Node, name, val string) {
	for i := range n.Attr {
		if n.Attr[i].Key == name {
			n.Attr[i].Val = val
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: name, Val: val})
}

func nsRemoveNodeAttr(n *html.Node, name string) {
	out := n.Attr[:0]
	for _, a := range n.Attr {
		if a.Key != name {
			out = append(out, a)
		}
	}
	n.Attr = out
}

func nsInnerHTML(n *html.Node) string {
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		_ = html.Render(&sb, c)
	}
	return sb.String()
}

// nsParseJSDate 近似 Date.parse：接受 ISO 8601（含无时区的本地形式按 UTC 处理）。
func nsParseJSDate(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UnixMilli(), true
		}
	}
	return 0, false
}

func parseNodeseekPage(src, topicID string, page int) (*nodeseekPage, error) {
	if len(src) > 10*1024*1024 {
		return nil, errors.New("NodeSeek 页面超过 10 MiB")
	}
	if !strings.Contains(src, "<html") {
		src = "<html><body>" + src + "</body></html>"
	}
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return nil, err
	}
	base, _ := url.Parse(nodeseekCanonicalURL(topicID, 1))
	titleLink := cascadia.Query(doc, selNodeseekTitle)
	wantPath := "/post-" + topicID + "-1"
	okTitle := false
	if titleLink != nil {
		href, _ := nsNodeAttr(titleLink, "href")
		if ref, err := url.Parse(href); err == nil {
			okTitle = base.ResolveReference(ref).Path == wantPath
		}
	}
	if !okTitle {
		title := ""
		if t := cascadia.Query(doc, nsSelTitleTag); t != nil {
			title = nsNodeText(t)
		}
		if reNodeseekVerifyTitle.MatchString(title) || cascadia.Query(doc, selNodeseekChallenge) != nil {
			return nil, errors.New(nodeseekVerificationMessage)
		}
		return nil, errors.New("NodeSeek 帖子不可读或页面结构已变化，未保存验证页或错误页")
	}
	pageBase, _ := url.Parse(nodeseekCanonicalURL(topicID, page))
	var posts []forumReply
	for _, item := range cascadia.QueryAll(doc, selNodeseekItems) {
		id, _ := nsNodeAttr(item, "id")
		if !nsReDigits.MatchString(id) {
			continue
		}
		article := cascadia.Query(item, selNodeseekArticle)
		if article == nil {
			return nil, errors.New("NodeSeek 楼层正文缺失，未将不完整页面保存为成功")
		}
		for _, el := range cascadia.QueryAll(article, selNodeseekMedia) {
			attr := "src"
			if el.Data == "a" {
				attr = "href"
			}
			val, _ := nsNodeAttr(el, attr)
			ref, err := url.Parse(val)
			if err != nil {
				nsRemoveNodeAttr(el, attr)
				continue
			}
			resolved := pageBase.ResolveReference(ref)
			if resolved.Scheme == "https" || resolved.Scheme == "http" {
				nsSetNodeAttr(el, attr, resolved.String())
			} else {
				nsRemoveNodeAttr(el, attr)
			}
		}
		content := extract.NodeseekHTMLToMarkdown(nsInnerHTML(article))
		if content == "" {
			return nil, errors.New("NodeSeek 楼层正文为空，可能需要登录后查看")
		}
		createdAt, ok := int64(0), false
		if tm := cascadia.Query(item, selNodeseekTime); tm != nil {
			dt, _ := nsNodeAttr(tm, "datetime")
			createdAt, ok = nsParseJSDate(dt)
		}
		if !ok {
			return nil, errors.New("NodeSeek 楼层时间缺失，页面结构可能已变化")
		}
		floor, _ := strconv.Atoi(id)
		posts = append(posts, forumReply{Floor: floor, Author: strings.TrimSpace(nsNodeText(cascadia.Query(item, selNodeseekAuthor))), Content: content, CreatedAt: createdAt})
	}
	hasMain := false
	for _, p := range posts {
		if p.Floor == 0 {
			hasMain = true
		}
	}
	if len(posts) == 0 || (page == 1 && !hasMain) {
		return nil, errors.New("NodeSeek 主楼或回复未加载，未保存空白页面")
	}
	pageCount := page
	pathRe := regexp.MustCompile(`^/post-` + regexp.QuoteMeta(topicID) + `-([1-9]\d*)$`)
	canon, _ := url.Parse(nodeseekCanonicalURL(topicID, 1))
	for _, link := range cascadia.QueryAll(doc, selNodeseekPaging) {
		href, _ := nsNodeAttr(link, "href")
		ref, err := url.Parse(href)
		if err != nil {
			continue
		}
		target := canon.ResolveReference(ref)
		if target.Scheme+"://"+target.Host != "https://www.nodeseek.com" {
			continue
		}
		if m := pathRe.FindStringSubmatch(target.Path); m != nil {
			if n, _ := strconv.Atoi(m[1]); n > pageCount {
				pageCount = n
			}
		}
	}
	node := ""
	if c := cascadia.Query(doc, selNodeseekCategory); c != nil {
		node = strings.TrimSpace(nsNodeText(c))
	}
	return &nodeseekPage{Title: strings.TrimSpace(nsNodeText(titleLink)), Node: node, Posts: posts, PageCount: pageCount}, nil
}

var nsReTopicID = regexp.MustCompile(`^[1-9]\d*$`)

// fetchNodeseekThread 对应同名函数：逐页串行，不并发轰击站点；任意一页失败则整次失败，保留验证重试入口。
func fetchNodeseekThread(ctx interface{ Err() error }, topicID string, fetchPage func(url string) (string, error)) (*forumThread, error) {
	if !nsReTopicID.MatchString(topicID) {
		return nil, errors.New("无效的 NodeSeek 帖子编号")
	}
	floors := map[int]forumReply{}
	rawFirst, err := fetchPage(nodeseekCanonicalURL(topicID, 1))
	if err != nil {
		return nil, err
	}
	first, err := parseNodeseekPage(rawFirst, topicID, 1)
	if err != nil {
		return nil, err
	}
	for _, p := range first.Posts {
		floors[p.Floor] = p
	}
	pageCount := first.PageCount
	for page := 2; page <= min(pageCount, 100); page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw, err := fetchPage(nodeseekCanonicalURL(topicID, page))
		if err != nil {
			return nil, err
		}
		next, err := parseNodeseekPage(raw, topicID, page)
		if err != nil {
			return nil, err
		}
		for _, p := range next.Posts {
			floors[p.Floor] = p
		}
		pageCount = max(pageCount, next.PageCount)
	}
	warning := ""
	if pageCount > 100 {
		warning = fmt.Sprintf("NodeSeek 共 %d 页，本次仅采集前 100 页，后续回复未入库", pageCount)
	}
	main := floors[0]
	var replies []forumReply
	for f, p := range floors {
		if f > 0 {
			replies = append(replies, p)
		}
	}
	nsSortForumReplies(replies)
	return &forumThread{
		Platform: "nodeseek", TopicID: topicID, Title: first.Title, Author: main.Author, Node: first.Node,
		CreatedAt: main.CreatedAt, ReplyCount: len(replies), Content: main.Content, Replies: replies,
		WebpageURL: nodeseekCanonicalURL(topicID, 1), WarningReason: warning,
	}, nil
}

func nsSortForumReplies(r []forumReply) {
	for i := 1; i < len(r); i++ {
		for j := i; j > 0 && r[j-1].Floor > r[j].Floor; j-- {
			r[j-1], r[j] = r[j], r[j-1]
		}
	}
}
