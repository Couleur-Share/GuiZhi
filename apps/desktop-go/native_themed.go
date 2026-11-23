package main

// 主题阅读（AI 阅读页）的原生服务适配器：services.ThemedReading → themedReading:* 处理器。
//
// 直连现有处理器（themed_ipc.go 的同步频道、themed_runtime_ipc.go 的生成与任务控制、article_ask.go 的搜索配置）：
// 任务的排队、执行、检查点、取消与续跑都在 themedRuntime 里，这里只负责「建参数 → 调处理器 → 解码成 DTO」，不另起一套状态。
// 任务进度来自 themedReading:progress 推送（与渲染层收到的同一份任务快照），经 pushHub 分发。
//
// 处理器的失败是返回值 {success:false, error}，不是 Go 错误；这里统一翻译成 error，界面据此提示并允许复制原因。
// 机密：任务的 error、页面素材的失败原因与警告来自模型 / 生图服务商的原始响应，可能回显密钥，会被界面原样显示并允许复制，
// 所以所有返回给界面的错误文本与这些字段在适配器出口统一抹掉当前设置里的全部 API Key（搜索密钥另外抹掉本次传入的候选值）。

import (
	"context"
	"encoding/json"
	"errors"

	"guizhi-mygo-shell/native/services"
)

type themedAdapter struct {
	b   *Bridge
	hub *pushHub
	cfg liveAIConfig
}

var _ services.ThemedReading = (*themedAdapter)(nil)

func newThemedAdapter(b *Bridge, hub *pushHub) *themedAdapter {
	return &themedAdapter{b: b, hub: hub, cfg: liveAIConfig{b: b}}
}

// themedReply 是 themedReading:* 处理器回包的并集：每个频道只填其中一部分。
type themedReply struct {
	Success    bool                         `json:"success"`
	Error      string                       `json:"error"`
	State      *services.ThemedState        `json:"state"`
	Task       *services.ThemedTask         `json:"task"`
	Tasks      []services.ThemedTask        `json:"tasks"`
	References []services.ThemedReference   `json:"references"`
	Preview    *services.ThemedPreview      `json:"preview"`
	Search     *services.ThemedSearchStatus `json:"search"`
	Path       string                       `json:"path"`
	Cancelled  bool                         `json:"cancelled"`
}

// scrub 抹掉文本里的 API Key：当前设置里的全部密钥，外加调用方额外给出的候选密钥。
func (a *themedAdapter) scrub(msg string, extra ...string) string {
	if msg == "" {
		return msg
	}
	keys := apiKeysOf(a.cfg.AISettings())
	for _, k := range extra {
		if len(k) >= 6 {
			keys = append(keys, k)
		}
	}
	return scrubKeys(msg, keys)
}

// fail 把错误归类：取消 / 超时原样透传；其余只保留抹掉密钥之后的文本。
func (a *themedAdapter) fail(err error, extra ...string) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	}
	msg := err.Error()
	if clean := a.scrub(msg, extra...); clean != msg {
		return errors.New(clean)
	}
	return err
}

// call 调处理器并解出回包；success=false 与解码失败、调用失败一样翻译成（已脱敏的）error。同时返回原始 JSON，供需要整包解码的 Get 使用。
func (a *themedAdapter) call(ctx context.Context, channel string, extra []string, args ...any) (themedReply, json.RawMessage, error) {
	raw, err := a.b.call(ctx, channel, args...)
	if err != nil {
		return themedReply{}, nil, a.fail(err, extra...)
	}
	var r themedReply
	if err := json.Unmarshal(raw, &r); err != nil {
		return themedReply{}, nil, a.fail(errors.New("解码 "+channel+" 的结果失败："+err.Error()), extra...)
	}
	if !r.Success {
		msg := r.Error
		if msg == "" {
			msg = channel + " 失败"
		}
		return r, raw, errors.New(a.scrub(msg, extra...))
	}
	return r, raw, nil
}

func (a *themedAdapter) scrubTask(t services.ThemedTask) services.ThemedTask {
	t.Error = a.scrub(t.Error)
	return t
}

func (a *themedAdapter) scrubTaskPtr(t *services.ThemedTask) *services.ThemedTask {
	if t == nil {
		return nil
	}
	c := a.scrubTask(*t)
	return &c
}

func (a *themedAdapter) scrubPage(p services.ThemedPage) services.ThemedPage {
	if len(p.Warnings) > 0 {
		warnings := make([]string, len(p.Warnings))
		for i, w := range p.Warnings {
			warnings[i] = a.scrub(w)
		}
		p.Warnings = warnings
	}
	if len(p.Assets) > 0 {
		assets := make([]services.ThemedAsset, len(p.Assets))
		for i, as := range p.Assets {
			as.Error = a.scrub(as.Error)
			assets[i] = as
		}
		p.Assets = assets
	}
	return p
}

func themedReqArg(req services.ThemedRequest) map[string]any {
	return map[string]any{"itemId": req.ItemID, "sourceKind": req.SourceKind}
}

// taskOf 取回包里的任务：处理器 success 却没有任务属于协议破坏，翻译成错误而不是零值任务。
func (a *themedAdapter) taskOf(channel string, r themedReply) (services.ThemedTask, error) {
	if r.Task == nil {
		return services.ThemedTask{}, errors.New(channel + " 没有返回任务")
	}
	return a.scrubTask(*r.Task), nil
}

func (a *themedAdapter) State(ctx context.Context, req services.ThemedRequest) (services.ThemedState, error) {
	r, _, err := a.call(ctx, "themedReading:state", nil, themedReqArg(req))
	if err != nil {
		return services.ThemedState{}, err
	}
	if r.State == nil {
		return services.ThemedState{}, errors.New("themedReading:state 没有返回状态")
	}
	st := *r.State
	st.Task = a.scrubTaskPtr(st.Task)
	return st, nil
}

func (a *themedAdapter) Get(ctx context.Context, in services.ThemedGetInput) (services.ThemedReadResult, error) {
	_, raw, err := a.call(ctx, "themedReading:get", nil, in)
	if err != nil {
		return services.ThemedReadResult{}, err
	}
	var out services.ThemedReadResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return services.ThemedReadResult{}, a.fail(errors.New("解码 themedReading:get 的结果失败：" + err.Error()))
	}
	if out.Page != nil {
		p := a.scrubPage(*out.Page)
		out.Page = &p
	}
	out.Task = a.scrubTaskPtr(out.Task)
	return out, nil
}

func (a *themedAdapter) References(ctx context.Context, req services.ThemedRequest) ([]services.ThemedReference, error) {
	r, _, err := a.call(ctx, "themedReading:references", nil, themedReqArg(req))
	if err != nil {
		return nil, err
	}
	out := make([]services.ThemedReference, len(r.References))
	for i, ref := range r.References {
		ref.Error = a.scrub(ref.Error)
		out[i] = ref
	}
	return out, nil
}

func (a *themedAdapter) Preview(ctx context.Context, req services.ThemedRequest, taskID string) (*services.ThemedPreview, error) {
	arg := themedReqArg(req)
	arg["taskId"] = taskID
	r, _, err := a.call(ctx, "themedReading:preview", nil, arg)
	if err != nil || r.Preview == nil {
		return nil, err
	}
	p := *r.Preview
	p.Page = a.scrubPage(p.Page)
	return &p, nil
}

func (a *themedAdapter) ListTasks(ctx context.Context) ([]services.ThemedTask, error) {
	r, _, err := a.call(ctx, "themedReading:listTasks", nil)
	if err != nil {
		return nil, err
	}
	out := make([]services.ThemedTask, len(r.Tasks))
	for i, t := range r.Tasks {
		out[i] = a.scrubTask(t)
	}
	return out, nil
}

// OnProgress 订阅 themedReading:progress：载荷是任务的 JSON 形状（*jsObj），解不出任务标识的推送直接忽略。
func (a *themedAdapter) OnProgress(fn func(services.ThemedTask)) func() {
	return a.hub.Subscribe(chThemedProgress, func(payload any) {
		raw, err := json.Marshal(payload)
		if err != nil {
			return
		}
		var task services.ThemedTask
		if json.Unmarshal(raw, &task) != nil || task.ID == "" {
			return
		}
		fn(a.scrubTask(task))
	})
}

func (a *themedAdapter) Generate(ctx context.Context, req services.ThemedRequest, opts services.ThemedOptions) (services.ThemedTask, error) {
	arg := themedReqArg(req)
	arg["options"] = opts
	r, _, err := a.call(ctx, "themedReading:generate", nil, arg)
	if err != nil {
		return services.ThemedTask{}, err
	}
	return a.taskOf("themedReading:generate", r)
}

func (a *themedAdapter) taskControl(ctx context.Context, channel, taskID string) (services.ThemedTask, error) {
	r, _, err := a.call(ctx, channel, nil, taskID)
	if err != nil {
		return services.ThemedTask{}, err
	}
	return a.taskOf(channel, r)
}

func (a *themedAdapter) Cancel(ctx context.Context, taskID string) (services.ThemedTask, error) {
	return a.taskControl(ctx, "themedReading:cancel", taskID)
}

func (a *themedAdapter) Resume(ctx context.Context, taskID string) (services.ThemedTask, error) {
	return a.taskControl(ctx, "themedReading:resume", taskID)
}

func (a *themedAdapter) ContinueOffline(ctx context.Context, taskID string) (services.ThemedTask, error) {
	return a.taskControl(ctx, "themedReading:continueOffline", taskID)
}

func (a *themedAdapter) RegenerateAsset(ctx context.Context, req services.ThemedRequest, assetID string) (services.ThemedTask, error) {
	arg := themedReqArg(req)
	arg["assetId"] = assetID
	r, _, err := a.call(ctx, "themedReading:regenerateAsset", nil, arg)
	if err != nil {
		return services.ThemedTask{}, err
	}
	return a.taskOf("themedReading:regenerateAsset", r)
}

func (a *themedAdapter) RestorePrevious(ctx context.Context, req services.ThemedRequest) error {
	_, _, err := a.call(ctx, "themedReading:restorePrevious", nil, themedReqArg(req))
	return err
}

func (a *themedAdapter) Remove(ctx context.Context, req services.ThemedRequest) error {
	_, _, err := a.call(ctx, "themedReading:remove", nil, themedReqArg(req))
	return err
}

func (a *themedAdapter) ExportHTML(ctx context.Context, in services.ThemedExportInput) (services.ThemedExportResult, error) {
	r, _, err := a.call(ctx, "themedReading:exportHtml", nil, in)
	if err != nil {
		return services.ThemedExportResult{}, err
	}
	return services.ThemedExportResult{Path: r.Path, Cancelled: r.Cancelled}, nil
}

func (a *themedAdapter) searchStatus(ctx context.Context, extra []string, args ...any) (services.ThemedSearchStatus, error) {
	r, _, err := a.call(ctx, "themedReading:searchConfig", extra, args...)
	if err != nil {
		return services.ThemedSearchStatus{}, err
	}
	if r.Search == nil {
		return services.ThemedSearchStatus{}, errors.New("themedReading:searchConfig 没有返回搜索状态")
	}
	st := *r.Search
	if st.ConfiguredProviders == nil {
		st.ConfiguredProviders = []string{}
	}
	return st, nil
}

func (a *themedAdapter) SearchStatus(ctx context.Context) (services.ThemedSearchStatus, error) {
	return a.searchStatus(ctx, nil)
}

func (a *themedAdapter) ConfigureSearch(ctx context.Context, in services.ThemedSearchConfigInput) (services.ThemedSearchStatus, error) {
	var extra []string
	if in.APIKey != nil {
		extra = append(extra, *in.APIKey)
	}
	return a.searchStatus(ctx, extra, in)
}
