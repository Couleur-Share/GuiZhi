package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"

	"guizhi-mygo-shell/native/services"
)

// 主题阅读适配器的集成测试：真实 SQLite（临时数据目录）、真实 themedReading:* 处理器与任务运行时；
// 模型调用走 Library.chatFn 钩子（不联网），页面与任务用 themedPages 直接落库。不碰真实的 %APPDATA%\GuiZhi。

const themedTestKey = "sk-test-secret-key" // configureAITestModels 写入设置的 API Key

type themedRig struct {
	a   *themedAdapter
	b   *Bridge
	l   *Library
	hub *pushHub
}

func newThemedRig(t *testing.T) *themedRig {
	t.Helper()
	_, b, l := newLibraryAIAdapterForTest(t)
	l.searchHTTP = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	hub := newPushHub()
	l.setPush(hub.Dispatch)
	l.registerThemedReading(b)
	l.registerArticleAsk(b)
	t.Cleanup(func() { l.themedRT.shutdown(context.Background()) })
	return &themedRig{a: newThemedAdapter(b, hub), b: b, l: l, hub: hub}
}

func (r *themedRig) configureModels(t *testing.T) {
	t.Helper()
	configureAITestModels(t, r.b, "http://127.0.0.1:1", "openai", "gpt-4o-mini")
}

func (r *themedRig) item(t *testing.T, title string) services.ThemedRequest {
	t.Helper()
	id := createOpsItem(t, r.b, map[string]any{"title": title, "content": title + " body paragraph for the theme page."})
	return services.ThemedRequest{ItemID: id, SourceKind: services.ThemedKindBody}
}

// seedPage 把一份页面落库：format 1 是旧格式（带整页 HTML，可读可导出），3 是当前格式。fresh 为 false 时指纹故意对不上（过期）。
func (r *themedRig) seedPage(t *testing.T, req services.ThemedRequest, role string, format int, fresh bool, task map[string]any) string {
	t.Helper()
	ctx := t.Context()
	it, err := r.l.themedItem(ctx, req.ItemID)
	if err != nil || it == nil {
		t.Fatalf("读条目：%v", err)
	}
	source, err := r.l.themedBuildSource(ctx, it, req.SourceKind)
	if err != nil {
		t.Fatalf("构建内容快照：%v", err)
	}
	if !fresh {
		source.set("fingerprint", strings.Repeat("0", 64))
	}
	id := themedUUID()
	now := time.Now().UnixMilli()
	version := map[string]any{
		"id": id, "itemId": req.ItemID, "sourceKind": req.SourceKind, "role": role, "formatVersion": format,
		"source":  source,
		"options": map[string]any{"style": "plain", "generateImages": false, "maxImages": 0},
		"assets":  []any{}, "warnings": []any{}, "design": nil, "createdAt": now, "updatedAt": now,
	}
	if format == 1 {
		// 旧格式设计：每个正文块一个空占位符（正文由渲染时按原文填入）。
		var html strings.Builder
		for _, blk := range source.get("blocks").(jsArr) {
			html.WriteString(`<div data-source-block="` + themedStr(blk, "id") + `"></div>`)
		}
		version["design"] = map[string]any{"direction": "d", "html": html.String(), "css": "", "assets": []any{}}
	} else {
		version["reconstruction"] = map[string]any{"notes": []any{}, "queries": []any{}, "references": []any{}, "draft": []any{}, "interactions": []any{}}
	}
	vObj := themedMustParse(t, version)
	var tObj *jsObj
	if task != nil {
		task["id"], task["itemId"], task["sourceKind"], task["versionId"] = themedUUID(), req.ItemID, req.SourceKind, id
		task["title"], task["createdAt"], task["updatedAt"] = it.Title, now, now
		tObj = themedMustParse(t, task)
	}
	if _, err := r.l.themedPages().saveVersion(ctx, vObj, tObj); err != nil {
		t.Fatalf("落库页面：%v", err)
	}
	return id
}

func themedMustParse(t *testing.T, v map[string]any) *jsObj {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := jsParse(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	return parsed.(*jsObj)
}

func themedWait(t *testing.T, label string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("等待超时：%s", label)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

type themedPushLog struct {
	mu    sync.Mutex
	tasks []services.ThemedTask
}

func (p *themedPushLog) add(t services.ThemedTask) {
	p.mu.Lock()
	p.tasks = append(p.tasks, t)
	p.mu.Unlock()
}

func (p *themedPushLog) states() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(p.tasks))
	for i, t := range p.tasks {
		out[i] = t.State
	}
	return out
}

func (p *themedPushLog) last() services.ThemedTask {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.tasks) == 0 {
		return services.ThemedTask{}
	}
	return p.tasks[len(p.tasks)-1]
}

func (r *themedRig) taskState(id string) string {
	tasks, err := r.a.ListTasks(context.Background())
	if err != nil {
		return "error:" + err.Error()
	}
	for _, t := range tasks {
		if t.ID == id {
			return t.State
		}
	}
	return "missing"
}

// 空库与无页面：状态、读取、参考资料、任务列表都是「空」而不是错误；模型未配置时读到 nil 的模型名；坏参数与不存在的条目被拒绝。
func TestNativeThemedEmptyAndInvalid(t *testing.T) {
	ctx := t.Context()
	r := newThemedRig(t)
	req := r.item(t, "Empty")

	st, err := r.a.State(ctx, req)
	if err != nil || st.HasPage || st.Task != nil || st.Stale {
		t.Fatalf("没有页面：%+v %v", st, err)
	}
	got, err := r.a.Get(ctx, services.ThemedGetInput{ThemedRequest: req})
	if err != nil || got.Page != nil || got.Previous || got.Task != nil || got.Models.Text != nil || got.Models.Image != nil || got.Search == nil {
		t.Fatalf("没有页面时 Get：%+v %v", got, err)
	}
	if got.Search.ConfiguredProviders == nil {
		t.Fatal("搜索服务列表应是空切片而不是 nil")
	}
	if refs, err := r.a.References(ctx, req); err != nil || refs == nil || len(refs) != 0 {
		t.Fatalf("参考资料：%+v %v", refs, err)
	}
	if tasks, err := r.a.ListTasks(ctx); err != nil || tasks == nil || len(tasks) != 0 {
		t.Fatalf("任务列表：%+v %v", tasks, err)
	}

	r.configureModels(t)
	got, _ = r.a.Get(ctx, services.ThemedGetInput{ThemedRequest: req, MetadataOnly: true})
	if got.Models.Text == nil || *got.Models.Text != "gpt-4o-mini" || got.Models.Image != nil {
		t.Fatalf("配置主文本模型后：文本模型有名字，生图模型仍没有：%+v", got.Models)
	}

	bad := services.ThemedRequest{ItemID: req.ItemID, SourceKind: "bogus"}
	if _, err := r.a.State(ctx, bad); err == nil || !strings.Contains(err.Error(), "无效") {
		t.Fatalf("坏的内容来源应被拒绝：%v", err)
	}
	missing := services.ThemedRequest{ItemID: "no-such-item", SourceKind: services.ThemedKindBody}
	if _, err := r.a.State(ctx, missing); err == nil || !strings.Contains(err.Error(), "条目不存在") {
		t.Fatalf("条目不存在：%v", err)
	}
	if _, err := r.a.Get(ctx, services.ThemedGetInput{ThemedRequest: missing}); err == nil {
		t.Fatal("条目不存在时 Get 应报错")
	}
	if err := r.a.Remove(ctx, missing); err == nil {
		t.Fatal("条目不存在时 Remove 应报错")
	}
	if _, err := r.a.Preview(ctx, req, "no-such-task"); err == nil || !strings.Contains(err.Error(), "不属于") {
		t.Fatalf("任务不存在：%v", err)
	}
	if _, err := r.a.Cancel(ctx, "no-such-task"); err == nil || !strings.Contains(err.Error(), "任务不存在") {
		t.Fatalf("取消不存在的任务：%v", err)
	}
}

// 页面经真实处理器往返：旧格式页面带整页文档、新格式页面不带；过期标记、上一版、最近任务和参考资料都正确解码。
func TestNativeThemedPagesRoundTrip(t *testing.T) {
	ctx := t.Context()
	r := newThemedRig(t)
	legacy := r.item(t, "Legacy")
	r.seedPage(t, legacy, services.ThemedRolePrevious, 1, true, nil)
	currentID := r.seedPage(t, legacy, services.ThemedRoleCurrent, 1, true, map[string]any{
		"state": "completed", "stage": "done", "completed": 3, "total": 3,
		"usage": map[string]any{"textCalls": 2, "imageCalls": 0, "imagesSaved": 0},
	})

	st, err := r.a.State(ctx, legacy)
	if err != nil || !st.HasPage || st.VersionID != currentID || st.FormatVersion != 1 || st.Stale || st.Task == nil || st.Task.State != services.ThemedTaskCompleted {
		t.Fatalf("状态：%+v %v", st, err)
	}
	got, err := r.a.Get(ctx, services.ThemedGetInput{ThemedRequest: legacy, InstanceID: "inst-1"})
	if err != nil || got.Page == nil || !got.Page.Legacy() || got.Page.ID != currentID || !got.Previous || got.Stale {
		t.Fatalf("旧格式页面：%+v %v", got, err)
	}
	if !strings.Contains(got.Document, "body paragraph") || !strings.Contains(got.Document, "inst-1") {
		t.Fatalf("旧格式页面应带整页文档（含阅读实例标识）：%.200q", got.Document)
	}
	if got.Page.Options.Style != "plain" || !got.Page.Options.Enhanced() || got.Page.Source.Title != "Legacy" {
		t.Fatalf("选项与来源：%+v", got.Page)
	}
	if got.Task == nil || got.Task.Usage == nil || got.Task.Usage.TextCalls != 2 || got.Task.Completed != 3 {
		t.Fatalf("最近任务与用量：%+v", got.Task)
	}
	meta, _ := r.a.Get(ctx, services.ThemedGetInput{ThemedRequest: legacy, MetadataOnly: true})
	if meta.Document != "" || meta.Page == nil {
		t.Fatalf("MetadataOnly 不给文档：%+v", meta)
	}

	// 新格式页面不带文档；指纹对不上就是过期。
	modern := r.item(t, "Modern")
	r.seedPage(t, modern, services.ThemedRoleCurrent, 3, false, nil)
	got, err = r.a.Get(ctx, services.ThemedGetInput{ThemedRequest: modern})
	if err != nil || got.Page == nil || got.Page.Legacy() || got.Document != "" || !got.Stale || got.Previous {
		t.Fatalf("新格式过期页面：%+v %v", got, err)
	}
	if st, _ := r.a.State(ctx, modern); !st.Stale || st.FormatVersion != 3 {
		t.Fatalf("状态应带过期标记：%+v", st)
	}
	tasks, err := r.a.ListTasks(ctx)
	if err != nil || len(tasks) != 1 || tasks[0].ItemID != legacy.ItemID {
		t.Fatalf("任务列表：%+v %v", tasks, err)
	}
}

// 失败原因来自服务商的原始响应，可能回显密钥：State / Get / ListTasks / 推送出口都要抹掉。
func TestNativeThemedScrubsTaskErrors(t *testing.T) {
	ctx := t.Context()
	r := newThemedRig(t)
	r.configureModels(t)
	req := r.item(t, "Scrub")
	leak := "provider said: invalid api key " + themedTestKey + " (401)"
	r.seedPage(t, req, services.ThemedRoleCurrent, 1, true, map[string]any{"state": "failed", "stage": "design", "completed": 1, "total": 3, "error": leak})

	raw, err := r.l.themedPages().listTasks(ctx)
	if err != nil || len(raw) != 1 || !strings.Contains(themedStr(raw[0], "error"), themedTestKey) {
		t.Fatalf("前提：库里的失败原因含密钥：%v %v", raw, err)
	}
	check := func(where, msg string) {
		t.Helper()
		if strings.Contains(msg, themedTestKey) || !strings.Contains(msg, "***") || !strings.Contains(msg, "invalid api key") {
			t.Fatalf("%s 应抹掉密钥并保留其余原因：%q", where, msg)
		}
	}
	st, err := r.a.State(ctx, req)
	if err != nil || st.Task == nil {
		t.Fatalf("%+v %v", st, err)
	}
	check("State", st.Task.Error)
	got, err := r.a.Get(ctx, services.ThemedGetInput{ThemedRequest: req})
	if err != nil || got.Task == nil {
		t.Fatalf("%+v %v", got, err)
	}
	check("Get", got.Task.Error)
	tasks, err := r.a.ListTasks(ctx)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("%+v %v", tasks, err)
	}
	check("ListTasks", tasks[0].Error)

	var log themedPushLog
	cancel := r.a.OnProgress(log.add)
	r.hub.Dispatch(chThemedProgress, map[string]any{"id": "t1", "itemId": req.ItemID, "state": "failed", "stage": "done", "error": leak})
	check("OnProgress", log.last().Error)
	// 解不出任务标识的推送被忽略；退订之后不再收到。
	r.hub.Dispatch(chThemedProgress, nil)
	r.hub.Dispatch(chThemedProgress, map[string]any{"state": "running"})
	if n := len(log.states()); n != 1 {
		t.Fatalf("无效推送应被忽略：%d", n)
	}
	cancel()
	r.hub.Dispatch(chThemedProgress, map[string]any{"id": "t2", "state": "running"})
	if n := len(log.states()); n != 1 {
		t.Fatalf("退订后不应再收到：%d", n)
	}
}

// 参考资料出口脱敏：References() 返回的 ThemedReference.Error 若回显了 API Key，必须在适配器出口抹掉。
func TestNativeThemedScrubsReferenceErrors(t *testing.T) {
	ctx := t.Context()
	r := newThemedRig(t)
	r.configureModels(t)
	req := r.item(t, "ScrubRef")
	r.seedPage(t, req, services.ThemedRoleCurrent, 3, true, nil)
	page, err := r.l.themedPages().get(ctx, req.ItemID, req.SourceKind, "")
	if err != nil || page == nil {
		t.Fatalf("前提：读回种子页面：%v %v", page, err)
	}
	leak := "fetch failed: key " + themedTestKey + " denied"
	page.set("reconstruction", themedMustParse(t, map[string]any{
		"notes":        []any{},
		"queries":      []any{},
		"interactions": []any{},
		"draft":        []any{map[string]any{"title": "H1", "markdown": "Body", "referenceIds": []any{"ref-1"}}},
		"references": []any{map[string]any{
			"id": "ref-1", "title": "Ref One", "url": "https://example.com/1",
			"text": "Ref body", "capturedAt": 1700000000000, "status": "ready", "error": leak,
		}},
	}))
	if _, err := r.l.db.Transaction(ctx, []sqlite.Statement{{SQL: "UPDATE themed_reading_versions SET payload=? WHERE id=?", Args: []any{themedJSON(page), themedStr(page, "id")}}}); err != nil {
		t.Fatalf("更新参考资料：%v", err)
	}
	refs, err := r.a.References(ctx, req)
	if err != nil || len(refs) != 1 {
		t.Fatalf("References：%v %+v", err, refs)
	}
	if strings.Contains(refs[0].Error, themedTestKey) || !strings.Contains(refs[0].Error, "***") {
		t.Fatalf("References.Error 应抹掉密钥：%q", refs[0].Error)
	}
}

// 错误脱敏：当前设置里的 API Key 与本次传入的候选搜索密钥都被抹掉，取消 / 超时原样透传。
func TestNativeThemedFailScrubs(t *testing.T) {
	r := newThemedRig(t)
	r.configureModels(t)
	if got := r.a.fail(errors.New("bad key " + themedTestKey)); got.Error() != "bad key ***" {
		t.Fatalf("设置里的密钥：%v", got)
	}
	if got := r.a.fail(errors.New("bad key tvly-candidate-key-123"), "tvly-candidate-key-123"); got.Error() != "bad key ***" {
		t.Fatalf("候选搜索密钥：%v", got)
	}
	if got := r.a.fail(errors.New("bad key abc"), "abc"); got.Error() != "bad key abc" {
		t.Fatalf("过短的候选值不抹（避免把普通文本替换成星号）：%v", got)
	}
	if got := r.a.fail(context.Canceled); !errors.Is(got, context.Canceled) {
		t.Fatalf("取消应原样透传：%v", got)
	}
	if got := r.a.fail(nil); got != nil {
		t.Fatalf("nil 应是 nil：%v", got)
	}
	plain := errors.New("no secrets here")
	if got := r.a.fail(plain); got != plain {
		t.Fatalf("无需脱敏时原样返回同一个错误：%v", got)
	}
}

// 生成的前置条件在运行时里校验：没有主文本模型、要生图却没有生图模型、已有活动任务都被拒绝，且不留下任务。
func TestNativeThemedGenerateRejections(t *testing.T) {
	ctx := t.Context()
	r := newThemedRig(t)
	req := r.item(t, "Reject")
	opts := services.ThemedOptions{Style: "plain", MaxImages: 1}

	if _, err := r.a.Generate(ctx, req, opts); err == nil || !strings.Contains(err.Error(), "主文本模型") {
		t.Fatalf("没有主文本模型：%v", err)
	}
	r.configureModels(t)
	withImages := opts
	withImages.GenerateImages = true
	if _, err := r.a.Generate(ctx, req, withImages); err == nil || !strings.Contains(err.Error(), "生图模型") {
		t.Fatalf("要生图却没有生图模型：%v", err)
	}
	withImages.MaxImages = 0
	if _, err := r.a.Generate(ctx, req, withImages); err == nil || !strings.Contains(err.Error(), "至少一张") {
		t.Fatalf("开了生图却把上限设为 0：%v", err)
	}
	bad := opts
	bad.MaxImages = 9
	if _, err := r.a.Generate(ctx, req, bad); err == nil || !strings.Contains(err.Error(), "0–5") {
		t.Fatalf("图片上限超过 5：%v", err)
	}
	bad = opts
	bad.Action = "bogus"
	if _, err := r.a.Generate(ctx, req, bad); err == nil {
		t.Fatal("未知的调整方式应被拒绝")
	}
	missing := services.ThemedRequest{ItemID: "no-such-item", SourceKind: services.ThemedKindBody}
	if _, err := r.a.Generate(ctx, missing, opts); err == nil || !strings.Contains(err.Error(), "条目不存在") {
		t.Fatalf("条目不存在：%v", err)
	}
	if tasks, _ := r.a.ListTasks(ctx); len(tasks) != 0 {
		t.Fatalf("被拒绝的生成不应留下任务：%+v", tasks)
	}
}

// 生成 → 失败：真实运行时排队并执行，模型错误里回显的密钥在任务、推送与读取出口都被抹掉；失败的任务可以继续（再失败一次）。
func TestNativeThemedGenerateFailureScrubbedEndToEnd(t *testing.T) {
	ctx := t.Context()
	r := newThemedRig(t)
	r.configureModels(t)
	req := r.item(t, "Fail")

	var calls int
	var mu sync.Mutex
	r.l.chatFn = func(c context.Context, cfg aiClientConfig, msgs []aiChatMessage, opts aiChatOptions) (aiChatResult, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return aiChatResult{}, errors.New("upstream rejected key " + cfg.APIKey)
	}
	var log themedPushLog
	cancel := r.a.OnProgress(log.add)
	defer cancel()

	task, err := r.a.Generate(ctx, req, services.ThemedOptions{Style: "plain", MaxImages: 0})
	if err != nil || task.ID == "" || task.State != services.ThemedTaskQueued || task.ItemID != req.ItemID || !task.Active() {
		t.Fatalf("应返回排队中的任务：%+v %v", task, err)
	}
	themedWait(t, "任务失败", func() bool { return r.taskState(task.ID) == services.ThemedTaskFailed })

	tasks, _ := r.a.ListTasks(ctx)
	if len(tasks) != 1 || tasks[0].Error == "" || strings.Contains(tasks[0].Error, themedTestKey) || !strings.Contains(tasks[0].Error, "***") {
		t.Fatalf("失败原因应保留文本并抹掉密钥：%+v", tasks)
	}
	raw, _ := r.l.themedPages().getTask(ctx, task.ID)
	if !strings.Contains(themedStr(raw, "error"), themedTestKey) {
		t.Fatalf("前提：库里的失败原因含密钥：%v", themedStr(raw, "error"))
	}
	themedWait(t, "失败的推送", func() bool { return log.last().State == services.ThemedTaskFailed })
	if last := log.last(); strings.Contains(last.Error, themedTestKey) || !strings.Contains(last.Error, "***") {
		t.Fatalf("推送里的失败原因应已脱敏：%q", last.Error)
	}
	if got := log.states(); got[0] != services.ThemedTaskQueued {
		t.Fatalf("推送序列应从排队开始：%v", got)
	}
	st, _ := r.a.State(ctx, req)
	if st.HasPage || st.Task == nil || st.Task.State != services.ThemedTaskFailed || !st.Task.Resumable() {
		t.Fatalf("失败的任务可继续、没有发布页面：%+v", st)
	}

	// 继续：任务回到排队并再次执行（这次仍失败）。
	themedWait(t, "运行时空闲", func() bool {
		r.l.themedRT.mu.Lock()
		defer r.l.themedRT.mu.Unlock()
		return !r.l.themedRT.pumping
	})
	mu.Lock()
	before := calls
	mu.Unlock()
	resumed, err := r.a.Resume(ctx, task.ID)
	if err != nil || resumed.ID != task.ID {
		t.Fatalf("继续：%+v %v", resumed, err)
	}
	themedWait(t, "再次执行", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return calls > before && r.taskState(task.ID) == services.ThemedTaskFailed
	})
}

// 取消与继续：执行中的任务被取消后进行中的模型请求被中止；已取消的任务可继续；同一份内容有活动任务时再生成被拒绝。
func TestNativeThemedCancelAndResume(t *testing.T) {
	ctx := t.Context()
	r := newThemedRig(t)
	r.configureModels(t)
	req := r.item(t, "Cancel")

	entered := make(chan struct{}, 8)
	aborted := make(chan struct{}, 8)
	r.l.chatFn = func(c context.Context, cfg aiClientConfig, msgs []aiChatMessage, opts aiChatOptions) (aiChatResult, error) {
		entered <- struct{}{}
		<-c.Done()
		aborted <- struct{}{}
		return aiChatResult{}, context.Cause(c)
	}
	wait := func(ch chan struct{}, what string) {
		t.Helper()
		select {
		case <-ch:
		case <-time.After(30 * time.Second):
			t.Fatalf("等待超时：%s", what)
		}
	}

	task, err := r.a.Generate(ctx, req, services.ThemedOptions{Style: "plain", MaxImages: 0})
	if err != nil {
		t.Fatal(err)
	}
	wait(entered, "模型请求发出")
	if _, err := r.a.Generate(ctx, req, services.ThemedOptions{Style: "plain"}); err == nil || !strings.Contains(err.Error(), "已有主题排版任务") {
		t.Fatalf("已有活动任务时再生成应被拒绝：%v", err)
	}
	if same, err := r.a.Resume(ctx, task.ID); err != nil || same.ID != task.ID || !same.Active() {
		t.Fatalf("活动任务原样返回、不报错：%+v %v", same, err)
	}

	cancelled, err := r.a.Cancel(ctx, task.ID)
	if err != nil || cancelled.State != services.ThemedTaskCancelled || cancelled.Error == "" || cancelled.Active() || !cancelled.Resumable() {
		t.Fatalf("取消：%+v %v", cancelled, err)
	}
	wait(aborted, "进行中的请求被中止")
	themedWait(t, "运行时空闲", func() bool {
		r.l.themedRT.mu.Lock()
		defer r.l.themedRT.mu.Unlock()
		return !r.l.themedRT.pumping
	})
	if got := r.taskState(task.ID); got != services.ThemedTaskCancelled {
		t.Fatalf("被取消的任务不应被运行时改写：%s", got)
	}
	if again, err := r.a.Cancel(ctx, task.ID); err != nil || again.State != services.ThemedTaskCancelled {
		t.Fatalf("再取消一次是无操作：%+v %v", again, err)
	}

	resumed, err := r.a.Resume(ctx, task.ID)
	if err != nil || resumed.State != services.ThemedTaskQueued {
		t.Fatalf("继续：%+v %v", resumed, err)
	}
	wait(entered, "继续后再次发出请求")
	if _, err := r.a.Cancel(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	wait(aborted, "第二次中止")
}

// 恢复上一版与删除：恢复把上一版换成当前版（原当前版退为上一版）；没有上一版是错误；删除后页面与上一版都没了，正文保留。
func TestNativeThemedRestoreAndRemove(t *testing.T) {
	ctx := t.Context()
	r := newThemedRig(t)
	req := r.item(t, "Restore")

	if err := r.a.RestorePrevious(ctx, req); err == nil || !strings.Contains(err.Error(), "没有上一可用版本") {
		t.Fatalf("没有上一版：%v", err)
	}
	prevID := r.seedPage(t, req, services.ThemedRolePrevious, 1, true, nil)
	curID := r.seedPage(t, req, services.ThemedRoleCurrent, 1, true, nil)

	if err := r.a.RestorePrevious(ctx, req); err != nil {
		t.Fatalf("恢复：%v", err)
	}
	got, _ := r.a.Get(ctx, services.ThemedGetInput{ThemedRequest: req, MetadataOnly: true})
	if got.Page == nil || got.Page.ID != prevID || !got.Previous {
		t.Fatalf("上一版应成为当前版：%+v（原当前版 %s）", got.Page, curID)
	}

	if err := r.a.Remove(ctx, req); err != nil {
		t.Fatalf("删除：%v", err)
	}
	got, _ = r.a.Get(ctx, services.ThemedGetInput{ThemedRequest: req, MetadataOnly: true})
	if got.Page != nil || got.Previous {
		t.Fatalf("删除后页面与上一版都没了：%+v", got)
	}
	var content string
	rows, err := r.l.db.Query(ctx, "SELECT content FROM knowledge_items WHERE id = ?", req.ItemID)
	if err != nil || len(rows.Values) != 1 {
		t.Fatalf("条目应仍在：%v", err)
	}
	content, _ = rows.Values[0][0].(string)
	if !strings.Contains(content, "body paragraph") {
		t.Fatalf("删除主题页不能动正文：%q", content)
	}
}

// 导出：保存对话框取消不是失败；写出的是自包含的单文件 HTML；没有页面时报错。
func TestNativeThemedExport(t *testing.T) {
	ctx := t.Context()
	r := newThemedRig(t)
	req := r.item(t, "Export")
	if _, err := r.a.ExportHTML(ctx, services.ThemedExportInput{ThemedRequest: req}); err == nil || !strings.Contains(err.Error(), "还没有可导出") {
		t.Fatalf("没有页面：%v", err)
	}
	r.seedPage(t, req, services.ThemedRoleCurrent, 1, true, nil)

	dest := ""
	r.l.saveFileDialog = func(title, defaultPath string, filters []dialogFilter) (string, error) { return dest, nil }
	res, err := r.a.ExportHTML(ctx, services.ThemedExportInput{ThemedRequest: req})
	if err != nil || !res.Cancelled || res.Path != "" {
		t.Fatalf("用户取消：%+v %v", res, err)
	}
	dest = filepath.Join(t.TempDir(), "out.html")
	res, err = r.a.ExportHTML(ctx, services.ThemedExportInput{ThemedRequest: req, StaticOnly: true})
	if err != nil || res.Cancelled || res.Path != dest {
		t.Fatalf("导出：%+v %v", res, err)
	}
	data, err := os.ReadFile(dest)
	if err != nil || !strings.Contains(string(data), "body paragraph") {
		t.Fatalf("导出文件：%v %.100q", err, data)
	}
	r.l.saveFileDialog = func(title, defaultPath string, filters []dialogFilter) (string, error) {
		return "", errors.New("dialog failed with " + themedTestKey)
	}
	r.configureModels(t)
	if _, err := r.a.ExportHTML(ctx, services.ThemedExportInput{ThemedRequest: req}); err == nil || strings.Contains(err.Error(), themedTestKey) {
		t.Fatalf("导出失败的原因应已脱敏：%v", err)
	}
}

// 搜索设置：读状态不带参数；写入后状态更新、可清除；坏的服务商被拒绝；候选密钥不出现在任何返回里。
func TestNativeThemedSearchConfig(t *testing.T) {
	ctx := t.Context()
	r := newThemedRig(t)

	st, err := r.a.SearchStatus(ctx)
	if err != nil || st.Configured || st.ConfiguredProviders == nil {
		t.Fatalf("初始状态：%+v %v", st, err)
	}
	key, enabled := "tvly-candidate-key-123", true
	st, err = r.a.ConfigureSearch(ctx, services.ThemedSearchConfigInput{Provider: "tavily", APIKey: &key, DefaultEnabled: &enabled})
	if err != nil || !st.Configured || !st.DefaultEnabled || st.Provider != "tavily" || len(st.ConfiguredProviders) != 1 {
		t.Fatalf("配置：%+v %v", st, err)
	}
	if raw, _ := json.Marshal(st); strings.Contains(string(raw), key) {
		t.Fatalf("状态不能带出密钥：%s", raw)
	}
	if again, err := r.a.SearchStatus(ctx); err != nil || again.Provider != "tavily" || !again.Configured {
		t.Fatalf("不带参数读到已保存的状态：%+v %v", again, err)
	}
	empty := ""
	st, err = r.a.ConfigureSearch(ctx, services.ThemedSearchConfigInput{APIKey: &empty})
	if err != nil || st.Configured {
		t.Fatalf("清除密钥：%+v %v", st, err)
	}
	if _, err := r.a.ConfigureSearch(ctx, services.ThemedSearchConfigInput{Provider: "bogus", APIKey: &key}); err == nil || strings.Contains(err.Error(), key) {
		t.Fatalf("坏的服务商：%v", err)
	}
	if got := (services.ThemedSearchConfigInput{Provider: "tavily", APIKey: &key}).String(); strings.Contains(got, key) {
		t.Fatalf("入参的文本形式不能含密钥：%s", got)
	}
}
