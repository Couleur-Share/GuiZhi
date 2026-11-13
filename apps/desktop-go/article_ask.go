package main

// 本文问答的联网查证与 IPC：articleAsk:context / articleAsk:search / articleAsk:cancelSearch。
// 对应 ipc/article-ask.ipc.ts 与 services/article-ask-search.ts。

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	whatwg "github.com/nlnwa/whatwg-url/url"
)

const (
	chArticleAskContext = "articleAsk:context"
	chArticleAskSearch  = "articleAsk:search"
	chArticleAskCancel  = "articleAsk:cancelSearch"

	articleSearchTimeout  = 90 * time.Second
	articleCaptureTimeout = 20 * time.Second
)

var reRequestID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

// articleAskDeps 是测试注入点；全空时走真实实现。
type articleAskDeps struct {
	search  func(ctx context.Context, query, scenario string) ([]readingSearchResult, error)
	capture func(ctx context.Context, req webCaptureRequest) webCaptureResult
}

type articleSearchOut struct {
	Success  bool            `json:"success"`
	Sources  []articleSource `json:"sources"`
	Warnings []string        `json:"warnings"`
}

var errArticleSearchTimeout = errors.New("本文联网查证超时，请稍后重试")

// abortReason 复刻 signal.throwIfAborted：取消与超时给出可读原因。
func abortReason(ctx context.Context) error {
	if errors.Is(context.Cause(ctx), errArticleSearchTimeout) {
		return errArticleSearchTimeout
	}
	return errors.New("搜索已取消")
}

// searchArticleWeb 串行查询 / 补抓，避免一次提问挤满共享网页采集队列。
func (l *Library) searchArticleWeb(ctx context.Context, queries []string) (*articleSearchOut, error) {
	search := l.articleAsk.search
	if search == nil {
		search = l.searchReadingWeb
	}
	capture := l.articleAsk.capture
	if capture == nil {
		capture = func(c context.Context, req webCaptureRequest) webCaptureResult { return l.captureWebPage(c, req, nil) }
	}
	warnings := []string{}
	type candidate struct {
		title, url string
		text       *string
	}
	var order []string
	candidates := map[string]candidate{}
	for _, query := range queries {
		if ctx.Err() != nil {
			return nil, abortReason(ctx)
		}
		results, err := search(ctx, query, "qa")
		if err != nil {
			if ctx.Err() != nil {
				return nil, abortReason(ctx)
			}
			warnings = append(warnings, err.Error())
			continue
		}
		for _, r := range results {
			u, perr := whatwg.Parse(r.URL)
			if perr != nil {
				return nil, errors.New("Invalid URL")
			}
			u.SetHash("")
			href := u.Href(false)
			if _, ok := candidates[href]; !ok {
				order = append(order, href)
				candidates[href] = candidate{title: r.Title, url: href, text: r.Text}
			}
		}
	}
	sources := []articleSource{}
	captures := 0
	for _, key := range order {
		if ctx.Err() != nil {
			return nil, abortReason(ctx)
		}
		if len(sources) >= 5 {
			break
		}
		r := candidates[key]
		text := ""
		if r.text != nil {
			text = jsTrim(*r.text)
		}
		if utf16Len(text) < 200 && captures < 3 {
			captures++
			func() {
				cctx, cancel := context.WithTimeout(ctx, articleCaptureTimeout)
				defer cancel()
				captured := capture(cctx, webCaptureRequest{URL: r.url, Purpose: "research", TaskID: "article-ask-" + strconv.FormatInt(time.Now().UnixMilli(), 10)})
				if captured.release != nil {
					defer captured.release()
				}
				if captured.Error != nil || !captured.Complete {
					warnings = append(warnings, "部分网页正文未能取得")
					return
				}
				text = captured.Markdown
			}()
			if ctx.Err() != nil {
				return nil, abortReason(ctx)
			}
		}
		if utf16Len(text) < 200 {
			warnings = append(warnings, "部分搜索结果缺少有效正文，未作为依据")
			continue
		}
		now := time.Now().UnixMilli()
		sources = append(sources, articleSource{Ordinal: 0, Kind: "web", Title: r.title, URL: r.url,
			Text: selectArticleContext(text, strings.Join(queries, " "), "", 2200).Text, CapturedAt: &now})
	}
	if len(sources) == 0 {
		warnings = append(warnings, "未取得有效联网资料，本次仅结合本文解释，未完成联网查证")
	}
	seen := map[string]bool{}
	uniq := []string{}
	for _, w := range warnings {
		if !seen[w] {
			seen[w] = true
			uniq = append(uniq, w)
		}
	}
	return &articleSearchOut{Success: true, Sources: sources, Warnings: uniq}, nil
}

type articleRunning struct {
	mu sync.Mutex
	m  map[string]context.CancelFunc
}

func (l *Library) registerArticleAsk(b *Bridge) {
	running := &articleRunning{m: map[string]context.CancelFunc{}}
	fail := func(action string, err error) any {
		msg := err.Error()
		l.paths.logAppError("articleAsk", action, msg, nil)
		return map[string]any{"success": false, "error": msg}
	}
	requestKey := func(id any) (string, error) {
		s, ok := id.(string)
		if !ok || !reRequestID.MatchString(s) {
			return "", errors.New("请求标识无效")
		}
		return s, nil
	}
	// 搜索服务配置属于主题阅读通道，但与本文问答共用同一份搜索服务，在此一并注册。
	b.handlers["themedReading:searchConfig"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		status, err := l.configureReadingSearch(ctx, jsArg(a, 0))
		if err != nil {
			msg := err.Error()
			l.paths.logAppError("themedReading", "searchConfig", msg, nil)
			return map[string]any{"success": false, "error": msg}, nil
		}
		return map[string]any{"success": true, "search": status}, nil
	}
	b.handlers[chArticleAskContext] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		out, err := l.articleContext(ctx, jsArg(a, 0))
		if err != nil {
			return fail("context", err), nil
		}
		return map[string]any{"success": true, "context": out}, nil
	}
	b.handlers[chArticleAskCancel] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		key, err := requestKey(jsArg(a, 0))
		if err != nil {
			return fail("cancel", err), nil
		}
		running.mu.Lock()
		if cancel := running.m[key]; cancel != nil {
			cancel()
		}
		running.mu.Unlock()
		return map[string]any{"success": true}, nil
	}
	b.handlers[chArticleAskSearch] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		input := jsArg(a, 0)
		key, err := requestKey(jsOptGet(input, "requestId"))
		if err != nil {
			return fail("search", err), nil
		}
		queries, bad := parseArticleQueries(jsGet(input, "queries"))
		if bad {
			return fail("search", errors.New("搜索词无效")), nil
		}
		running.mu.Lock()
		if len(running.m) > 0 {
			running.mu.Unlock()
			return fail("search", errors.New("已有本文搜索正在执行，请稍后重试")), nil
		}
		sctx, cancel := context.WithCancel(ctx)
		tctx, tcancel := context.WithTimeoutCause(sctx, articleSearchTimeout, errArticleSearchTimeout)
		running.m[key] = cancel
		running.mu.Unlock()
		defer func() {
			tcancel()
			cancel()
			running.mu.Lock()
			delete(running.m, key)
			running.mu.Unlock()
		}()
		out, err := l.searchArticleWeb(tctx, queries)
		if err != nil {
			return fail("search", err), nil
		}
		return out, nil
	}
}

// parseArticleQueries 校验 1–2 个非空、不超过 400 字的搜索词，并去重（Set 保持首次出现顺序）。
func parseArticleQueries(v any) ([]string, bool) {
	arr, ok := v.(jsArr)
	if !ok || len(arr) == 0 || len(arr) > 2 {
		return nil, true
	}
	var out []string
	seen := map[string]bool{}
	for _, q := range arr {
		s, ok := q.(string)
		if !ok || jsTrim(s) == "" || utf16Len(s) > 400 {
			return nil, true
		}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out, false
}
