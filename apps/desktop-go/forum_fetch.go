package main

// 论坛抓取器的公共部分，对应 services/import/forum-types.ts 与各抓取器里重复的重试/退避/错误翻译骨架。
// 抓取器的 JSON 都按 jsvalue 模型读取，保持 TS 里 typeof / ?? / ?. 的宽松语义。

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
)

const forumCanceledMsg = "已取消"

type forumReply struct {
	Floor     int
	Author    string
	Content   string
	CreatedAt int64
	ReplyTo   *forumReplyTo
}

type forumThread struct {
	Platform       string
	TopicID        string
	Title          string
	Author         string
	Node           string
	CreatedAt      int64
	ReplyCount     int
	Content        string
	Replies        []forumReply
	SummaryReplies []forumReply // 为 nil 时用 Replies
	ReplyRetention string       // "all" | "op-only"，空按 all
	WebpageURL     string
	WarningReason  string
}

// forumRetryDelays 对应各抓取器的 RETRY_DELAYS_MS：瞬时故障的退避间隔，长度即额外尝试次数。
var forumRetryDelays = []time.Duration{1500 * time.Millisecond, 4000 * time.Millisecond}

// forumDeps 汇总可注入的依赖；零值即生产行为。
type forumDeps struct {
	// fetchJSON 抓取并解析 JSON；为空时走 Library 的安全抓取器
	fetchJSON func(ctx context.Context, url string) (any, error)
	// fetchAuthenticatedJSON 站点需要验证或登录时，经其专属浏览器会话重试（渲染切片接入前为空）
	fetchAuthenticatedJSON func(ctx context.Context, url string) (any, error)
	// retryDelays 非 nil 时覆盖默认退避；空切片表示不重试
	retryDelays []time.Duration
	// 2Libra 评论分页参数（测试用）
	commentPageSize, maxCommentPages int
}

func (d forumDeps) delays() []time.Duration {
	if d.retryDelays != nil {
		return d.retryDelays
	}
	return forumRetryDelays
}

func (l *Library) forumGetJSON(deps forumDeps) func(ctx context.Context, url string) (any, error) {
	if deps.fetchJSON != nil {
		return deps.fetchJSON
	}
	return func(ctx context.Context, url string) (any, error) {
		fetcher := l.webFetcher
		if fetcher == nil {
			fetcher = defaultSafeFetcher
		}
		raw, err := fetcher.fetchJSON(ctx, url, safeRequestOptions{})
		if err != nil {
			return nil, err
		}
		return jsParse(string(raw))
	}
}

// forumWait 对应可被取消打断的 wait。
func forumWait(ctx context.Context, d time.Duration) error {
	if ctx.Err() != nil {
		return errors.New(forumCanceledMsg)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return errors.New(forumCanceledMsg)
	}
}

var reHTTP5xx = regexp.MustCompile(`HTTP 5\d\d`)

// isForumTransient 对应 v2ex / discourse 的 isTransientFailure：5xx 与连接层故障。
func isForumTransient(msg string) bool {
	return reHTTP5xx.MatchString(msg) || msg == "请求超时" || msg == "连接被中断"
}

var reHTTP5xxCapture = regexp.MustCompile(`HTTP (5\d\d)`)

// ---- jsvalue 取值辅助（对应 TS 的 typeof / ?. / ??） ----

func fjObj(v any) *jsObj { o, _ := v.(*jsObj); return o }

func fjArr(v any) (jsArr, bool) { a, ok := v.(jsArr); return a, ok }

func fjStr(v any) (string, bool) { s, ok := v.(string); return s, ok }

func fjNum(v any) (float64, bool) {
	n, ok := v.(float64)
	return n, ok
}

// fjTrim 对应 `x?.trim()`：字符串取 trim 后的值，其余按空串。
func fjTrim(v any) string {
	if s, ok := v.(string); ok {
		return jsTrim(s)
	}
	return ""
}

// forumNormalizeText 对应各抓取器的 normalizeText：统一换行、去首尾空白。
func forumNormalizeText(v any) string {
	s, _ := v.(string)
	return jsTrim(strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(s))
}

// forumMillis 把 Date.parse 结果换成毫秒；失败回退到当前时间。
func forumISOMillis(v any) int64 {
	if s, ok := v.(string); ok && s != "" {
		if ms, ok := legacyDateParse(s); ok {
			return ms
		}
	}
	return time.Now().UnixMilli()
}

func forumErrMsg(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
