package main

// 对应 services/web-capture/{web-capture,web-error,web-extract-article(结果组装)}.ts 与
// shared/utils/web-scope.ts 里的纯逻辑：规范化入口网址、错误归类、提取结果组装。
// 网络与编排见 web_capture.go。

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"

	whatwg "github.com/nlnwa/whatwg-url/url"

	"guizhi-mygo-shell/extract"
)

// webExtractEngine 对应 WEB_EXTRACT_ENGINE：提取规则有不兼容变化时递增，写入采集结果与原文版本。
const webExtractEngine = "readability/1"

type webCaptureError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

type webParagraph struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// webCaptureResult 对应 WebCaptureResult（snapshot 随微信快照切片接入）。
type webCaptureResult struct {
	TaskID         string           `json:"taskId"`
	EntryURL       string           `json:"entryUrl"`
	FinalURL       string           `json:"finalUrl"`
	Title          string           `json:"title"`
	Author         string           `json:"author"`
	PublishedAt    *int64           `json:"publishedAt"`
	DateConfidence string           `json:"dateConfidence"`
	Markdown       string           `json:"markdown"`
	Links          []string         `json:"links"`
	Paragraphs     []webParagraph   `json:"paragraphs"`
	ContentHash    string           `json:"contentHash"`
	CapturedAt     int64            `json:"capturedAt"`
	EngineVersion  string           `json:"engineVersion"`
	Complete       bool             `json:"complete"`
	Truncated      bool             `json:"truncated"`
	Warnings       []string         `json:"warnings"`
	Error          *webCaptureError `json:"error,omitempty"`
	// 公众号原文快照（有序对象，直接并入版本 payload）、其资产文件名，以及释放资产租约的回调
	Snapshot       *jsObj   `json:"-"`
	SnapshotAssets []string `json:"-"`
	release        func()
}

// canonicalWebURL 对应 canonicalWebUrl：只接受不含账号口令的 http(s)，去掉 hash。
func canonicalWebURL(raw string) (string, error) {
	u, err := whatwg.Parse(raw)
	if err != nil {
		return "", errors.New("Invalid URL")
	}
	if p := u.Protocol(); (p != "http:" && p != "https:") || u.Username() != "" || u.Password() != "" {
		return "", errors.New("请输入不含账号密码的 HTTP(S) 网址")
	}
	u.SetHash("")
	return u.Href(false), nil
}

var (
	reErrTimeout    = regexp.MustCompile(`(?i)timeout|超时`)
	reErrCanceled   = regexp.MustCompile(`(?i)取消|abort`)
	reErrSecurity   = regexp.MustCompile(`(?i)内网|本地网络|private|internal network|local network|超出.*范围|不支持.*协议|目录.*越界`)
	reErrCaptcha    = regexp.MustCompile(`验证码|人机验证`)
	reErrLogin      = regexp.MustCompile(`登录`)
	reErrRestricted = regexp.MustCompile(`HTTP (403|429)|拒绝访问|限流`)
	reErrIncomplete = regexp.MustCompile(`超过.*(MiB|限制)|超限|预算已用尽`)
	reErrURL        = regexp.MustCompile(`https?://[^` + jsSpaceClass + `]+`)
)

var webErrorCodes = map[string]bool{"canceled": true, "timeout": true, "security": true, "captcha": true, "login": true, "restricted": true, "incomplete": true, "network": true, "empty": true}

// webCauseCode 让调用方带上 TS 里 `cause.webCaptureCode` 的显式归类。
type webCauseError struct {
	msg  string
	Code string
}

func (e *webCauseError) Error() string { return e.msg }

func newWebCaptureFailure(msg, code string) error { return &webCauseError{msg: msg, Code: code} }

// webCaptureErrorOf 对应 webCaptureError(error)：按消息文本归类，隐藏 URL 里的查询参数与账号口令。
func webCaptureErrorOf(err error) webCaptureError {
	text := ""
	if err != nil {
		text = err.Error()
	}
	return classifyWebError(text, explicitWebCode(err))
}

func explicitWebCode(err error) string {
	var c *webCauseError
	if errors.As(err, &c) {
		return c.Code
	}
	return ""
}

func classifyWebError(text, explicit string) webCaptureError {
	code := "network"
	switch {
	case reErrTimeout.MatchString(text):
		code = "timeout"
	case reErrCanceled.MatchString(text):
		code = "canceled"
	case reErrSecurity.MatchString(text):
		code = "security"
	case reErrCaptcha.MatchString(text):
		code = "captcha"
	case reErrLogin.MatchString(text):
		code = "login"
	case reErrRestricted.MatchString(text):
		code = "restricted"
	case reErrIncomplete.MatchString(text):
		code = "incomplete"
	}
	if webErrorCodes[explicit] {
		code = explicit
	}
	msg := reErrURL.ReplaceAllStringFunc(text, func(raw string) string {
		w, werr := whatwg.Parse(raw)
		if werr != nil {
			return "[网址]"
		}
		return w.Protocol() + "//" + w.Host() + w.Pathname()
	})
	return webCaptureError{Code: code, Message: utf16Slice(msg, 1000), Retryable: code == "network" || code == "timeout" || code == "incomplete"}
}

var reParagraphSplit = regexp.MustCompile(`\n[` + jsSpaceClass + `]*\n`)

// webExtractedFrom 对应 extractWebArticle 的结果组装：段落、内容哈希、完整性、截断警告与错误。
func webExtractedFrom(a extract.Article, pageURL string) webCaptureResult {
	title := a.Title
	if title == "" {
		title = pageURL
	}
	r := webCaptureResult{
		Title: title, Author: a.Author, PublishedAt: a.PublishedAt, DateConfidence: "unknown",
		Markdown: a.Markdown, Truncated: a.Truncated, Warnings: []string{}, Links: []string{},
	}
	if a.PublishedAt != nil {
		r.DateConfidence = "exact"
	}
	r.Paragraphs = []webParagraph{}
	for i, p := range reParagraphSplit.Split(a.Markdown, -1) {
		if jsTrim(p) != "" {
			r.Paragraphs = append(r.Paragraphs, webParagraph{ID: fmt.Sprintf("p%d", i+1), Text: p})
		}
	}
	sum := sha256.Sum256([]byte(a.Markdown))
	r.ContentHash = hex.EncodeToString(sum[:])
	if a.Truncated {
		r.Warnings = append(r.Warnings, "正文超过 200,000 字符，已截断")
	}
	if code := a.ErrorCode; code != "" {
		msg := map[string]string{
			"login": "网页需要登录", "restricted": "网站拒绝访问或限制请求频率", "captcha": "网页要求完成验证码", "empty": "网页没有可保存的正文",
		}[code]
		if code == "network" {
			msg = "网站返回 HTTP " + fmt.Sprint(a.Status)
		}
		r.Error = &webCaptureError{Code: code, Message: msg, Retryable: code == "network"}
	}
	r.Complete = r.Error == nil && !a.Truncated
	return r
}

// extractWebPage 对应提取线程里的 extractWebArticle(html, url, status).result。
func extractWebPage(htmlText, pageURL string, status int) webCaptureResult {
	return webExtractedFrom(extract.ExtractWebArticle(htmlText, pageURL, status), pageURL)
}

var jsSpaceClass = strings.Join([]string{`\t\n\v\f\r `, `\x{a0}`, `\x{1680}`, `\x{2000}-\x{200a}`, `\x{2028}\x{2029}`, `\x{202f}`, `\x{205f}`, `\x{3000}`, `\x{feff}`}, "")
