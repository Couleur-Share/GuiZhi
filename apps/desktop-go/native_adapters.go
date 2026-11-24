package main

// 原生界面（native/…）与主包之间的适配层。
//
// native/screens、native/app 只依赖 native/services 的窄接口；这里把 Library 现有的 IPC 处理器接成这些接口。
// 适配器经 Bridge.call 调用已登记的处理器（与渲染进程同一份逻辑、同一套错误），再把 JSON 结果解码成 DTO：
// 行为只有一个来源，DTO 与处理器输出的对应关系由适配器测试用真实处理器守住。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sync"

	"github.com/egoist/mygo"

	"guizhi-mygo-shell/native/services"
)

// call 以渲染进程的方式调用已登记的 IPC 处理器：args 按 JSON 数组编码，返回处理器结果的 JSON。
func (b *Bridge) call(ctx context.Context, channel string, args ...any) (json.RawMessage, error) {
	encoded, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("编码 %s 的参数失败：%w", channel, err)
	}
	out, err := b.Invoke(ctx, channel, string(encoded))
	if err != nil {
		return nil, err
	}
	return json.RawMessage(out), nil
}

// callAs 调用处理器并把结果解码成 T。解码失败带上频道名，便于定位 DTO 与处理器输出对不上的地方。
func callAs[T any](ctx context.Context, b *Bridge, channel string, args ...any) (T, error) {
	var out T
	raw, err := b.call(ctx, channel, args...)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("解码 %s 的结果失败：%w", channel, err)
	}
	return out, nil
}

// pushHub 把 Library 的单监听推送（setPush）分发给多个订阅者，供原生界面按频道订阅。
// 回调在推送方 goroutine 上同步执行，订阅者只做轻量工作（通常是去抖地安排一次刷新）。
type pushHub struct {
	mu   sync.RWMutex
	next int
	subs map[string]map[int]func(payload any)
}

func newPushHub() *pushHub { return &pushHub{subs: map[string]map[int]func(any){}} }

// Subscribe 订阅一个频道，返回取消函数（可重复调用）。
func (h *pushHub) Subscribe(channel string, fn func(payload any)) (cancel func()) {
	h.mu.Lock()
	h.next++
	id := h.next
	if h.subs[channel] == nil {
		h.subs[channel] = map[int]func(any){}
	}
	h.subs[channel][id] = fn
	h.mu.Unlock()
	return func() {
		h.mu.Lock()
		delete(h.subs[channel], id)
		h.mu.Unlock()
	}
}

// Dispatch 把一次推送交给该频道的全部订阅者；没有订阅者时静默丢弃，与窗口未创建时的 pushMain 一致。
func (h *pushHub) Dispatch(channel string, payload any) {
	h.mu.RLock()
	fns := make([]func(any), 0, len(h.subs[channel]))
	for _, fn := range h.subs[channel] {
		fns = append(fns, fn)
	}
	h.mu.RUnlock()
	for _, fn := range fns {
		fn(payload)
	}
}

// importsAdapter 把导入队列相关的处理器接成 services.Imports。
type importsAdapter struct {
	b   *Bridge
	hub *pushHub
}

func (a importsAdapter) List(ctx context.Context, q services.ImportQuery) (services.ImportPage, error) {
	arg := map[string]any{"origin": orStr(q.Origin, "all"), "status": orStr(q.Status, "all"), "query": q.Query}
	if q.PageSize > 0 {
		arg["pageSize"] = q.PageSize
	}
	if q.Cursor != "" {
		arg["cursor"] = q.Cursor
	}
	return callAs[services.ImportPage](ctx, a.b, "import:list", arg)
}

func (a importsAdapter) OnChange(fn func()) func() {
	return a.hub.Subscribe(chImportChanged, func(any) { fn() })
}

// OnTaskChange 把 import:changed 的推送（任务的 JSON 形状，与渲染层收到的相同）解成 ImportTask；
// 解不出 id 的推送（空载荷、格式不对）直接忽略。
func (a importsAdapter) OnTaskChange(fn func(services.ImportTask)) func() {
	return a.hub.Subscribe(chImportChanged, func(payload any) {
		raw, err := json.Marshal(payload)
		if err != nil {
			return
		}
		var task services.ImportTask
		if json.Unmarshal(raw, &task) != nil || task.ID == "" {
			return
		}
		fn(task)
	})
}

func (a importsAdapter) QueueState(ctx context.Context) (services.ImportQueueState, error) {
	return callAs[services.ImportQueueState](ctx, a.b, "import:queueState")
}

func (a importsAdapter) SetPaused(ctx context.Context, paused bool) (services.ImportQueueState, error) {
	channel := "import:resume"
	if paused {
		channel = "import:pause"
	}
	return callAs[services.ImportQueueState](ctx, a.b, channel)
}

func (a importsAdapter) Cancel(ctx context.Context, id string) error {
	_, err := a.b.call(ctx, "import:cancel", id)
	return err
}

// Retry 的处理器在任务不存在时返回 null：这里翻译成 found=false，由调用方决定怎么提示。
func (a importsAdapter) Retry(ctx context.Context, id string, opts services.ImportRetryOptions) (bool, error) {
	args := []any{id}
	if opts != (services.ImportRetryOptions{}) {
		args = append(args, opts)
	}
	task, err := callAs[*services.ImportTask](ctx, a.b, "import:retry", args...)
	return task != nil, err
}

func (a importsAdapter) Remove(ctx context.Context, id string) error {
	_, err := a.b.call(ctx, "import:remove", id)
	return err
}

func (a importsAdapter) PreviewClearTerminal(ctx context.Context, q services.ImportClearQuery) (int, error) {
	out, err := callAs[struct {
		Count int `json:"count"`
	}](ctx, a.b, "import:clearTerminalPreview", q)
	return out.Count, err
}

func (a importsAdapter) ClearTerminal(ctx context.Context, q services.ImportClearQuery) (int, error) {
	out, err := callAs[struct {
		Count int `json:"count"`
	}](ctx, a.b, "import:clearTerminal", q)
	return out.Count, err
}

func (a importsAdapter) Enqueue(ctx context.Context, inputs []services.EnqueueImportInput) ([]services.ImportTask, error) {
	return callAs[[]services.ImportTask](ctx, a.b, "import:enqueue", inputs)
}

func (a importsAdapter) SelectFiles(ctx context.Context) ([]string, error) {
	return callAs[[]string](ctx, a.b, "dialog:selectImportFiles")
}

// ItemFacts 经 knowledge:get（与渲染层 window.api.knowledge.get 同一个处理器）读条目，只取成果卡与来源刷新对照用的标题与数字；
// 处理器在条目不存在时返回 null，这里翻译成 found=false。整篇正文只在这里过一遍，不进界面。
func (a importsAdapter) ItemFacts(ctx context.Context, id string) (services.ImportItemFacts, bool, error) {
	item, err := callAs[*struct {
		ID         string `json:"id"`
		Title      string `json:"title"`
		Content    string `json:"content"`
		Transcript string `json:"transcript"`
		Tags       []struct {
			Name string `json:"name"`
		} `json:"tags"`
	}](ctx, a.b, "knowledge:get", id)
	if err != nil || item == nil {
		return services.ImportItemFacts{}, false, err
	}
	tags := make([]string, len(item.Tags))
	for i, tag := range item.Tags {
		tags[i] = tag.Name
	}
	return services.ComputeImportItemFacts(item.ID, item.Title, item.Content, item.Transcript, tags), true, nil
}

func orStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// logAdapter 把原生界面的错误留痕交给渲染进程用的同一个 log:appError 处理器：
// 同样的字段截断（范围/动作 64、内容 2000）、同样的家目录脱敏、同样写进 error.log 与轮转，
// 两套界面的日志格式只有一个来源。
type logAdapter struct{ b *Bridge }

func (a logAdapter) AppError(scope, action, message string) {
	encoded, err := json.Marshal([]any{map[string]string{"scope": scope, "action": action, "message": message}})
	if err != nil {
		return
	}
	a.b.Send("log:appError", string(encoded))
}

// newNativeServices 装配原生界面依赖的全部能力。
func newNativeServices(b *Bridge, hub *pushHub) services.Services {
	return services.Services{
		Imports:      importsAdapter{b: b, hub: hub},
		DocSite:      docSiteAdapter{b: b},
		PlatformAuth: platformAuthAdapter{b: b},
		Library:      libraryAdapter{b: b, hub: hub},
		Inbox:        newInboxAdapter(b),
		Shortcuts:    shortcutsAdapter{b: b},
		Solver:       solverAdapter{b: b},
		Updater:      updaterAdapter{b: b, hub: hub},
		Data:         dataAdapter{b: b},
		Backup:       backupAdapter{b: b},
		CaptureTools: captureToolsAdapter{b: b, hub: hub},
		MCP:          mcpAdapter{b: b},
		AI:           aiSettingsAdapter{b: b},
		Ask:          newAskAdapter(b),
		Wiki:         newWikiAdapter(b),
		Themed:       newThemedAdapter(b, hub),
		Migration:    migrationAdapter{b: b},
		Log:          logAdapter{b: b},
		OpenExternal: webLinkOpener(func(u string) error { return mygo.Shell.OpenExternal(u) }),
	}
}

// webLinkOpener 把「打开系统浏览器」包成只放行 http / https 的版本：条目的来源链接来自采集到的网页，不能让它借 ShellExecute 启动 file:、ms-settings: 之类的协议。
func webLinkOpener(open func(string) error) func(string) error {
	return func(raw string) error {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("不支持的链接：只能打开 http 或 https 地址")
		}
		return open(u.String())
	}
}
