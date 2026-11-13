package main

// 对应 ipc/import.ipc.ts：导入队列与任务管理频道。

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	whatwg "github.com/nlnwa/whatwg-url/url"
)

const chImportChanged = "import:changed"

var captureStrategies = []string{"standard", "authenticated"}

func isImportCaptureStrategy(v any) bool {
	s, ok := v.(string)
	return ok && slices.Contains(captureStrategies, s)
}

func isCommentLimit(v any) bool {
	n, ok := v.(float64)
	return ok && (n == 0 || n == 10 || n == 20 || n == 50)
}

// validImportInput 对应 validInput。
func validImportInput(v any) bool {
	if !jsIsObject(v) {
		return false
	}
	kind, _ := jsGet(v, "kind").(string)
	if kind != "text" && kind != "file" && kind != "url" {
		return false
	}
	input, ok := jsGet(v, "input").(string)
	if !ok || jsTrim(input) == "" {
		return false
	}
	strategy, limit := jsGet(v, "captureStrategy"), jsGet(v, "commentLimit")
	if strategy != jsUndefined && !isImportCaptureStrategy(strategy) {
		return false
	}
	if limit != jsUndefined && !isCommentLimit(limit) {
		return false
	}
	if strategy == "authenticated" && (kind != "url" || detectPlatformCapturePlatform(input) == "") {
		return false
	}
	if n, _ := limit.(float64); n > 0 && (strategy != "authenticated" || kind != "url" || detectPlatformCapturePlatform(input) == "") {
		return false
	}
	return true
}

// dialogFilter 对应对话框的一条文件类型过滤。
type dialogFilter struct {
	Name string
	Exts []string
}

var importFileFilters = []dialogFilter{
	{"支持的文件", []string{"txt", "md", "markdown", "png", "jpg", "jpeg", "webp", "gif", "bmp", "mp3", "wav", "m4a", "aac", "ogg", "flac", "mp4", "webm", "mkv", "mov", "avi"}},
	{"文本文件", []string{"txt", "md", "markdown"}},
	{"图片", []string{"png", "jpg", "jpeg", "webp", "gif", "bmp"}},
	{"音频", []string{"mp3", "wav", "m4a", "aac", "ogg", "flac"}},
	{"视频", []string{"mp4", "webm", "mkv", "mov", "avi"}},
	{"All Files", []string{"*"}},
}

func (l *Library) registerImport(b *Bridge) {
	q := newImportQueue(l, l.defaultExtract, func(task map[string]any) { l.pushMain(chImportChanged, task) })
	l.imports = q
	if l.extractOverride != nil {
		q.extract = l.extractOverride
	}

	b.handlers["import:enqueue"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		inputs, ok := jsArg(a, 0).(jsArr)
		if !ok {
			return nil, errors.New("采集参数不合法")
		}
		authenticated := 0
		for _, in := range inputs {
			if !validImportInput(in) {
				return nil, errors.New("采集参数不合法")
			}
			if jsGet(in, "captureStrategy") == "authenticated" {
				authenticated++
			}
		}
		if authenticated > 50 {
			return nil, errors.New("单次最多创建 50 个登录态采集任务")
		}
		return q.Enqueue(ctx, inputs)
	}
	b.handlers["import:queueState"] = func(context.Context, []json.RawMessage) (any, error) { return q.state(), nil }
	b.handlers["import:pause"] = func(context.Context, []json.RawMessage) (any, error) { return q.Pause(), nil }
	b.handlers["import:resume"] = func(context.Context, []json.RawMessage) (any, error) { return q.Resume(), nil }
	b.handlers["import:cancel"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		id, ok := jsArg(a, 0).(string)
		if !ok {
			return false, nil
		}
		return q.Cancel(ctx, id)
	}
	b.handlers["import:retry"] = func(ctx context.Context, a []json.RawMessage) (any, error) { return l.importRetry(ctx, q, a) }
	b.handlers["import:remove"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		id, err := wikiBind(jsArg(a, 0))
		if err != nil {
			return nil, err
		}
		return l.importTaskRemove(ctx, id)
	}
	b.handlers["import:clearFinished"] = func(ctx context.Context, _ []json.RawMessage) (any, error) {
		return l.importTaskClearFinished(ctx)
	}
	b.handlers["import:clearTerminalPreview"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		n, err := l.importTaskCountTerminal(ctx, jsArg(a, 0))
		if err != nil {
			return nil, err
		}
		return map[string]any{"count": n, "preview": true}, nil
	}
	b.handlers["import:clearTerminal"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		n, err := l.importTaskClearTerminal(ctx, jsArg(a, 0))
		if err != nil {
			return nil, err
		}
		return map[string]any{"count": n, "preview": false}, nil
	}
	b.handlers["dialog:selectImportFiles"] = func(context.Context, []json.RawMessage) (any, error) {
		if l.pickFiles == nil {
			return []string{}, nil
		}
		paths, err := l.pickFiles(true, importFileFilters)
		if err != nil || paths == nil {
			return []string{}, err
		}
		return paths, nil
	}
}

func (l *Library) importRetry(ctx context.Context, q *importQueue, a []json.RawMessage) (any, error) {
	idV, opts := jsArg(a, 0), jsArg(a, 1)
	id, ok := idV.(string)
	if !ok || id == "" {
		return nil, errors.New("任务 ID 不合法")
	}
	get := func(k string) any { return jsOptGet(opts, k) }
	if v := get("captureStrategy"); v != jsUndefined && !isImportCaptureStrategy(v) {
		return nil, errors.New("采集策略不合法")
	}
	if v := get("commentLimit"); v != jsUndefined && !isCommentLimit(v) {
		return nil, errors.New("评论数量不合法")
	}
	if v := get("forceDuplicate"); v != jsUndefined {
		if _, isBool := v.(bool); !isBool {
			return nil, errors.New("重复采集参数不合法")
		}
	}
	task, err := l.importTaskGet(ctx, id)
	if err != nil || task == nil {
		return nil, err
	}
	verify := get("verifyWeb")
	if verify != jsUndefined {
		if _, isBool := verify.(bool); !isBool {
			return nil, errors.New("网页验证参数不合法")
		}
	}
	strategyV := get("captureStrategy")
	if verify == true {
		if !needsWebVerification(task) || strategyV == "authenticated" {
			return nil, errors.New("该任务不支持通用网页验证")
		}
		u, err := whatwg.Parse(task["sourceInput"].(string))
		if err != nil {
			return nil, errors.New("Invalid URL")
		}
		if u.Protocol() != "https:" || u.Username() != "" || u.Password() != "" || (u.Port() != "" && u.Port() != "443") {
			return nil, errors.New("网页验证只支持 HTTPS 标准端口")
		}
	}
	sourceKind, _ := task["sourceKind"].(string)
	sourceInput, _ := task["sourceInput"].(string)
	var o retryOptions
	requestedStrategy := ""
	if isImportCaptureStrategy(strategyV) {
		requestedStrategy = strategyV.(string)
		o.captureStrategy = &requestedStrategy
	}
	if requestedStrategy == "authenticated" && (sourceKind != "url" || detectPlatformCapturePlatform(sourceInput) == "") {
		return nil, errors.New("该任务不支持登录态采集")
	}
	if isCommentLimit(get("commentLimit")) {
		n := int64(get("commentLimit").(float64))
		o.commentLimit = &n
	}
	resulting, _ := task["captureStrategy"].(string)
	if requestedStrategy != "" {
		resulting = requestedStrategy
	}
	if o.commentLimit != nil && *o.commentLimit > 0 && (resulting != "authenticated" || sourceKind != "url" || detectPlatformCapturePlatform(sourceInput) == "") {
		return nil, errors.New("热门评论只支持小红书、抖音或 LINUX DO 登录态任务")
	}
	if v, isBool := get("forceDuplicate").(bool); isBool {
		o.forceDuplicate = &v
	}
	q.mu.Lock()
	if verify == true {
		q.webVerify[id] = true
	} else {
		delete(q.webVerify, id)
	}
	q.mu.Unlock()
	retried, err := q.Retry(ctx, id, o)
	if err != nil || retried == nil {
		q.mu.Lock()
		delete(q.webVerify, id)
		q.mu.Unlock()
	}
	if err != nil {
		return nil, err
	}
	return retried, nil
}
