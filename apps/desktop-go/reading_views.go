package main

// 主题阅读视图：对应 services/themed-reading/v3-views.ts。
//
// Electron 版用 WebContentsView 把「受控外壳 + 沙箱 iframe」叠在主窗口上；Go 壳没有这类原生子视图，
// 改为：Go 生成外壳文档并保管视图状态与全部校验（命令 / 事件 / 心跳 / 故障），渲染进程的 electron 替身
// （shim/reading-view-host.ts）只负责把外壳放进一个 sandbox="allow-scripts" 的 iframe 并中继 postMessage。
// 页面里的素材一律内联为 data: URL（外壳 CSP 本就允许 data:），不需要自定义协议。
// 与 Electron 版的已知差异：iframe 与主界面是否分进程取决于 WebView2 的站点隔离；
// 心跳与五秒无响应故障检测仍在，但同步死循环无法由壳强杀，只能由 WebView2 的“页面无响应”处理。

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	chThemedViewCreate  = "themedReading:viewCreate"
	chThemedViewUpdate  = "themedReading:viewUpdate"
	chThemedViewCommand = "themedReading:viewCommand"
	chThemedViewDestroy = "themedReading:viewDestroy"
	chThemedViewEvent   = "themedReading:viewEvent"
	chReadingViewBridge = "readingView:bridge"
	chReadingViewCmd    = "readingView:command"

	readingViewMax        = 3
	readingHeartbeatEvery = 250 * time.Millisecond
)

var readingHeartbeatBudget = 5 * time.Second

var (
	reReadingRequestID = regexp.MustCompile(`^[\w-]{1,80}$`)
	reReadingAnchor    = regexp.MustCompile(`^[a-zA-Z][\w-]{0,199}$`)
)

type readingView struct {
	id       string
	page     *jsObj
	preview  bool
	enabled  bool
	ready    bool
	nonce    string
	sentAt   time.Time
	fault    string
	release  func()
	stop     chan struct{}
	stopOnce sync.Once
}

type readingViewHost struct {
	l     *Library
	push  func(channel string, payload any) // 非空时覆盖 l.push（运行检查用）
	mu    sync.Mutex
	views map[string]*readingView
}

func (l *Library) readingViews() *readingViewHost {
	l.readingViewsOnce.Do(func() { l.readingViewHost = &readingViewHost{l: l, views: map[string]*readingView{}} })
	return l.readingViewHost
}

func finite(v any) (float64, bool) {
	f, ok := v.(float64)
	return f, ok && !math.IsNaN(f) && !math.IsInf(f, 0)
}

// validateReadingViewCommand 对应同名函数：命令必须是白名单类型且参数在范围内。
func validateReadingViewCommand(c any) error {
	typ, _ := jsGet(c, "type").(string)
	if !vTruthy(c) || (typ != "appearance" && typ != "find" && typ != "anchor" && typ != "scroll" && typ != "interaction") {
		return errors.New("阅读命令无效")
	}
	switch typ {
	case "appearance":
		theme, _ := jsGet(c, "theme").(string)
		size, ok := finite(jsGet(c, "fontSize"))
		family := jsGet(c, "fontFamily")
		fs, isStr := family.(string)
		if (theme != "light" && theme != "dark") || !ok || size < 12 || size > 32 || (family != jsUndefined && (!isStr || utf16Len(fs) > 1000)) {
			return errors.New("阅读外观参数无效")
		}
	case "find":
		q, qok := jsGet(c, "query").(string)
		rid := jsGet(c, "requestId")
		ridStr, ridOK := rid.(string)
		idx, iok := finite(jsGet(c, "index"))
		if !qok || utf16Len(q) > 1000 || (rid != jsUndefined && (!ridOK || !reReadingRequestID.MatchString(ridStr))) || !iok || idx != math.Trunc(idx) || math.Abs(idx) > 9007199254740991 {
			return errors.New("阅读查找参数无效")
		}
	case "anchor":
		id, ok := jsGet(c, "id").(string)
		if !ok || !reReadingAnchor.MatchString(id) {
			return errors.New("阅读章节无效")
		}
	case "scroll":
		top, ok := finite(jsGet(c, "top"))
		if !ok || top < 0 || top > 2000000 {
			return errors.New("阅读位置无效")
		}
	case "interaction":
		if _, ok := jsGet(c, "enabled").(bool); !ok {
			return errors.New("阅读交互开关无效")
		}
	}
	return nil
}

func validateReadingViewBounds(b any) error {
	if !vTruthy(b) {
		return errors.New("阅读视图位置无效")
	}
	for _, k := range []string{"x", "y", "width", "height"} {
		if _, ok := finite(jsGet(b, k)); !ok {
			return errors.New("阅读视图位置无效")
		}
	}
	w, _ := finite(jsGet(b, "width"))
	h, _ := finite(jsGet(b, "height"))
	if w < 0 || h < 0 || w > 20000 || h > 20000 {
		return errors.New("阅读视图位置无效")
	}
	if _, ok := jsGet(b, "visible").(bool); !ok {
		return errors.New("阅读视图位置无效")
	}
	return nil
}

// pusher 返回事件出口：运行检查用自带出口，正常视图走主窗口推送。
func (h *readingViewHost) pusher() func(string, any) {
	if h.push != nil {
		return h.push
	}
	h.l.pushMu.RLock()
	defer h.l.pushMu.RUnlock()
	return h.l.push
}

func (h *readingViewHost) get(id string) (*readingView, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e := h.views[id]
	if e == nil {
		return nil, errors.New("阅读视图已失效或不属于当前窗口")
	}
	return e, nil
}

func (h *readingViewHost) emit(e *readingView, typ string, value any) {
	push := h.pusher()
	if push == nil {
		return
	}
	ev := newObj()
	ev.set("viewId", e.id)
	ev.set("type", typ)
	if value != nil {
		ev.set("value", value)
	}
	push(chThemedViewEvent, ev)
}

func (h *readingViewHost) sendCommand(e *readingView, command any) {
	push := h.pusher()
	if push != nil {
		m := newObj()
		m.set("viewId", e.id)
		m.set("command", command)
		push(chReadingViewCmd, m)
	}
}

func (e *readingView) halt() { e.stopOnce.Do(func() { close(e.stop) }) }

func (h *readingViewHost) stopEntry(e *readingView) {
	e.halt()
	if e.release != nil {
		e.release()
	}
}

// fault 对应 fault：只处理一次；停止心跳、通知渲染进程（替身据此移除 iframe 并回落静态正文）。
func (h *readingViewHost) fault(e *readingView, message string) {
	h.mu.Lock()
	if e.fault != "" {
		h.mu.Unlock()
		return
	}
	e.fault = message
	h.mu.Unlock()
	e.halt()
	v := newObj()
	v.set("message", message)
	h.emit(e, "fault", v)
}

func (h *readingViewHost) closeAll() {
	h.mu.Lock()
	list := make([]*readingView, 0, len(h.views))
	for _, e := range h.views {
		list = append(list, e)
	}
	h.views = map[string]*readingView{}
	h.mu.Unlock()
	for _, e := range list {
		h.stopEntry(e)
	}
}

// readingRelay 是外壳里固定的中继代码：沙箱 iframe 的消息带上视图 ID 转交宿主页面，宿主的命令转给沙箱 iframe。
// 它取代 Electron 版 preload/reading-view.ts（无 ipcRenderer，所以用 postMessage 与替身通话）。
func readingRelay(viewID string) string {
	id, _ := json.Marshal(viewID)
	return `<script>(function(){var v=` + strings.ReplaceAll(string(id), "<", "\\u003c") + `;var f=document.getElementById('reading');
window.addEventListener('message',function(e){var d=e.data;
if(e.source===f.contentWindow&&e.origin==='null'&&d&&d.reading===3){parent.postMessage({gzReadingView:v,data:d},'*');return;}
if(e.source===parent&&d&&d.gzReadingCommand&&d.viewId===v){f.contentWindow.postMessage({reading:3,command:d.gzReadingCommand},'*');}});})();</script>`
}

// document 生成外壳文档：素材内联、套 TS 原版的内层文档与外壳，再注入中继。
func (h *readingViewHost) document(e *readingView) (string, error) {
	assetMap := newObj()
	for _, a := range themedArr(e.page.get("assets")) {
		if themedStr(a, "status") != "ready" {
			continue
		}
		data, mime, err := h.l.readThemeAsset(a)
		if err != nil {
			continue // 与 TS 的素材路由一致：读不到按 404，页面里显示为缺图
		}
		assetMap.set(themedStr(a, "id"), "data:"+mime+";base64,"+base64.StdEncoding.EncodeToString(data))
	}
	out, err := themedCall("v3Inner", themedJSON(e.page), themedJSON(assetMap), e.enabled && !e.preview, e.preview, true)
	if err != nil {
		return "", err
	}
	inner, err := themedEnvelope(out)
	if err != nil {
		return "", err
	}
	wrapper, err := themedCall("v3Wrapper", inner)
	if err != nil {
		return "", err
	}
	idx := strings.LastIndex(wrapper, "</body>")
	if idx < 0 {
		return "", errors.New("阅读外壳文档格式异常")
	}
	return wrapper[:idx] + readingRelay(e.id) + wrapper[idx:], nil
}

// themedEnvelope 解开引擎的 {ok} / {issue} 信封：可修复的页面问题按 TS 的 ReadingPageError 以其 message 抛出。
func themedEnvelope(raw string) (string, error) {
	var env struct {
		OK    *string `json:"ok"`
		Issue *struct {
			Message string `json:"message"`
		} `json:"issue"`
	}
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		return "", err
	}
	if env.Issue != nil {
		return "", errors.New(env.Issue.Message)
	}
	if env.OK == nil {
		return "", errors.New("阅读页文档生成失败")
	}
	return *env.OK, nil
}

func (h *readingViewHost) create(ctx context.Context, raw any) (map[string]any, error) {
	l := h.l
	req, err := parseThemedRequest(raw)
	if err != nil {
		return nil, err
	}
	stored, err := l.themedPages().getVersion(ctx, jsToString(jsGet(raw, "versionId")))
	if err != nil {
		return nil, err
	}
	if stored == nil || themedStr(stored, "itemId") != req.itemID || themedStr(stored, "sourceKind") != req.kind || themedNum(stored.get("formatVersion")) != 3 {
		return nil, errors.New("阅读版本不存在或不属于当前文章")
	}
	preview := jsGet(raw, "preview") == true
	if themedStr(stored, "role") == "working" && !vTruthy(jsGet(raw, "preview")) {
		return nil, errors.New("未发布页面只能静态预览")
	}
	page := stored
	if vTruthy(jsGet(raw, "preview")) {
		page = themedPreviewVersion(stored)
	}
	if page == nil {
		return nil, errors.New("尚无可预览正文")
	}
	inspected, err := l.themedInspectImages(page)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	// 渲染进程重载后遗留的故障实例不应占满名额
	for id, e := range h.views {
		if e.fault != "" && len(h.views) >= readingViewMax {
			delete(h.views, id)
			go h.stopEntry(e)
		}
	}
	if len(h.views) >= readingViewMax {
		h.mu.Unlock()
		return nil, errors.New("阅读视图数量超过限制")
	}
	release, err := retainAssetFiles(themedAssetFiles(inspected))
	if err != nil {
		h.mu.Unlock()
		return nil, err
	}
	e := &readingView{id: themedUUID(), page: inspected, preview: preview, enabled: jsGet(raw, "scriptsEnabled") != false && !preview, release: release, stop: make(chan struct{})}
	h.views[e.id] = e
	h.mu.Unlock()
	doc, err := h.document(e)
	if err != nil {
		h.mu.Lock()
		delete(h.views, e.id)
		h.mu.Unlock()
		h.stopEntry(e)
		return nil, err
	}
	h.startHeartbeat(e)
	return map[string]any{"success": true, "viewId": e.id, "document": doc}, nil
}

func (h *readingViewHost) startHeartbeat(e *readingView) {
	h.mu.Lock()
	e.nonce, e.sentAt = themedUUID(), time.Now()
	h.mu.Unlock()
	go func() {
		t := time.NewTicker(readingHeartbeatEvery)
		defer t.Stop()
		for {
			select {
			case <-e.stop:
				return
			case <-t.C:
			}
			h.mu.Lock()
			if !e.sentAt.IsZero() && time.Since(e.sentAt) > readingHeartbeatBudget {
				h.mu.Unlock()
				h.fault(e, "阅读交互超过五秒未响应，已停止交互")
				return
			}
			if e.nonce == "" {
				e.nonce, e.sentAt = themedUUID(), time.Now()
			}
			nonce := e.nonce
			h.mu.Unlock()
			c := newObj()
			c.set("type", "heartbeat")
			c.set("nonce", nonce)
			h.sendCommand(e, c)
		}
	}()
}

// receive 对应 e.receive：校验来自阅读页的消息，合法才转成事件发给渲染进程。
func (h *readingViewHost) receive(viewID string, data any) {
	h.mu.Lock()
	e := h.views[viewID]
	h.mu.Unlock()
	if e == nil {
		return
	}
	typ, ok := jsGet(data, "type").(string)
	if !vTruthy(data) || !ok {
		return
	}
	if typ == "heartbeat" {
		h.mu.Lock()
		if v, _ := jsGet(data, "value").(string); v == e.nonce {
			e.nonce, e.sentAt = "", time.Time{}
		}
		h.mu.Unlock()
		return
	}
	if s, ok := jsStringify(data); !ok || utf16Len(s) > 64000 {
		return
	}
	v := jsGet(data, "value")
	if typ == "ready" {
		h.mu.Lock()
		e.ready = true
		h.mu.Unlock()
		h.emit(e, "ready", nil)
	}
	if typ == "key" {
		if k, _ := v.(string); k == "find" || k == "escape" {
			h.emit(e, "key", k)
		}
	}
	if typ == "layout" {
		top, tok := finite(jsGet(v, "top"))
		height, hok := finite(jsGet(v, "height"))
		heads, aok := jsGet(v, "headings").(jsArr)
		valid := vTruthy(v) && tok && top >= 0 && top <= 2000000 && hok && height >= 0 && height <= 2000000 && aok && len(heads) <= 200
		if valid {
			for _, hd := range heads {
				id, idOK := jsGet(hd, "id").(string)
				text, tOK := jsGet(hd, "text").(string)
				if !vTruthy(hd) || !idOK || !reReadingAnchor.MatchString(id) || !tOK || utf16Len(text) > 200 {
					valid = false
					break
				}
			}
		}
		if valid {
			h.emit(e, "layout", v)
		}
	}
	if typ == "selection" {
		text, tok := jsGet(v, "text").(string)
		x, xok := finite(jsGet(v, "x"))
		y, yok := finite(jsGet(v, "y"))
		if tok && utf16Len(text) <= 4000 && xok && yok && math.Abs(x) < 20000 && math.Abs(y) < 20000 {
			h.emit(e, "selection", v)
		}
	}
	if typ == "find" {
		c, ok := finite(jsGet(v, "count"))
		if ok && c == math.Trunc(c) && c >= 0 && c <= 10000 {
			h.emit(e, "find", v)
		}
	}
	if typ == "image" {
		id := jsGet(v, "id")
		for _, a := range themedArr(e.page.get("assets")) {
			if themedStr(a, "status") == "ready" && jsGet(a, "id") == id && id != jsUndefined {
				m := newObj()
				m.set("id", id)
				h.emit(e, "image", m)
				break
			}
		}
	}
	if typ == "fault" {
		label := "交互运行失败"
		if d, ok := e.page.get("design").(*jsObj); ok {
			for _, s := range themedArr(d.get("scripts")) {
				if themedStr(s, "id") == jsToString(jsGet(v, "id")) && jsGet(v, "id") != jsUndefined {
					label = "交互 " + jsToString(jsGet(v, "id")) + " 运行失败"
					break
				}
			}
		}
		msg := jsGet(v, "message")
		if isNullish(msg) {
			msg = "未知错误"
		}
		h.fault(e, label+"："+utf16Head(jsToString(msg), 1000))
	}
}

func (l *Library) registerReadingViews(b *Bridge, wrap func(string, func(context.Context, []json.RawMessage) (any, error)) func(context.Context, []json.RawMessage) (any, error)) {
	h := l.readingViews()
	if b.sends == nil {
		b.sends = map[string]func([]json.RawMessage){}
	}
	b.handlers[chThemedViewCreate] = wrap("viewCreate", func(ctx context.Context, a []json.RawMessage) (any, error) {
		return h.create(ctx, jsArg(a, 0))
	})
	b.handlers[chThemedViewUpdate] = wrap("viewUpdate", func(ctx context.Context, a []json.RawMessage) (any, error) {
		raw := jsArg(a, 0)
		if _, err := h.get(jsToString(jsGet(raw, "viewId"))); err != nil {
			return nil, err
		}
		if err := validateReadingViewBounds(jsGet(raw, "bounds")); err != nil {
			return nil, err
		}
		return map[string]any{"success": true}, nil
	})
	b.handlers[chThemedViewCommand] = wrap("viewCommand", func(ctx context.Context, a []json.RawMessage) (any, error) {
		raw := jsArg(a, 0)
		e, err := h.get(jsToString(jsGet(raw, "viewId")))
		if err != nil {
			return nil, err
		}
		command := jsGet(raw, "command")
		if err := validateReadingViewCommand(command); err != nil {
			return nil, err
		}
		if themedStr(command, "type") == "interaction" {
			h.mu.Lock()
			if e.fault != "" {
				h.mu.Unlock()
				return nil, errors.New("故障阅读实例已停止，请重新启用交互")
			}
			e.enabled = jsGet(command, "enabled") == true && !e.preview
			e.ready = false
			h.mu.Unlock()
			doc, err := h.document(e)
			if err != nil {
				return nil, err
			}
			return map[string]any{"success": true, "document": doc}, nil
		}
		h.mu.Lock()
		faulted := e.fault != ""
		h.mu.Unlock()
		if !faulted {
			h.sendCommand(e, command)
		}
		return map[string]any{"success": true}, nil
	})
	b.handlers[chThemedViewDestroy] = wrap("viewDestroy", func(ctx context.Context, a []json.RawMessage) (any, error) {
		id := jsToString(jsArg(a, 0))
		e, err := h.get(id)
		if err != nil {
			return nil, err
		}
		h.mu.Lock()
		delete(h.views, id)
		h.mu.Unlock()
		h.stopEntry(e)
		return map[string]any{"success": true}, nil
	})
	// 阅读页 → 主进程的消息（Electron 版是 preload 里的 ipcRenderer.send；这里由替身中继，载荷多一个 viewId）。
	b.sends[chReadingViewBridge] = func(a []json.RawMessage) {
		msg := jsArg(a, 0)
		h.receive(jsToString(jsGet(msg, "viewId")), msg)
	}
}
