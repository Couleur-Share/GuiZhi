package main

// Wiki 模块（目录、搜索、详情与编辑、关系图谱、全文编译、更新建议）的原生服务适配器：services.Wiki → wiki:* 处理器与 aiflow.WikiDriver。
//
// 读写页面直连 wiki:* 处理器（保存前的历史版本、手动编辑标记、出链重建、全文索引都在那里）；
// 全文编译沿用渲染层的检查点协议：任务、分块进度与落盘都在 wiki:compiler 处理器里，
// aiflow.WikiDriver 只负责「取分块 → 调模型 → 清洗 → 交回」的循环，请求出口是 bridgeTransport（ai:httpRequest 代理，SSRF 守卫共用）。
//
// 驱动器在适配器里只有一个实例：取消一个任务要中止「正在驱动它的那个驱动器」的进行中请求，所以所有 RunCompile / ControlCompile 必须共用它。
// 它读的是实时设置（liveAIConfig）而不是创建时的快照：任务创建时记下模型身份串，续跑与逐块调用前都要核对，
// 编译中途换了模型就必须能被发现，而不是继续用旧快照调旧模型。
//
// 机密：返回给界面的错误文本都会抹掉当前设置里的全部 API Key。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"guizhi-mygo-shell/aiflow"
	"guizhi-mygo-shell/native/services"
)

// liveAIConfig 每次取设置都重新读 settings:get；读不到时返回零值设置（等价于「未配置」）。
type liveAIConfig struct{ b *Bridge }

func (c liveAIConfig) AISettings() aiflow.AISettings {
	raw, err := callAs[map[string]any](context.Background(), c.b, "settings:get")
	if err != nil {
		return aiflow.AISettings{}
	}
	return aiflow.NormalizeAISettings(raw)
}

type wikiAdapter struct {
	b      *Bridge
	cfg    liveAIConfig
	driver *aiflow.WikiDriver
}

var _ services.Wiki = (*wikiAdapter)(nil)

func newWikiAdapter(b *Bridge) *wikiAdapter {
	cfg := liveAIConfig{b: b}
	runner := &aiflow.Runner{
		Client: &aiflow.Client{Transport: bridgeTransport{b: b}},
		Config: cfg,
		Usage:  bridgeUsageSink{b: b, ctx: context.Background()},
	}
	return &wikiAdapter{b: b, cfg: cfg, driver: &aiflow.WikiDriver{Runner: runner, Store: wikiCompilerStore{b: b}}}
}

// mapErr 把错误归类成 services.Wiki 约定的形态：模型未配置给哨兵，其余只保留抹掉 API Key 之后的文本。
func (a *wikiAdapter) mapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	case aiflow.IsAborted(err):
		return context.Canceled
	case aiflow.IsNotConfigured(err):
		return services.ErrAINotConfigured
	}
	msg := err.Error()
	if clean := scrubKeys(msg, apiKeysOf(a.cfg.AISettings())); clean != msg {
		return errors.New(clean)
	}
	return err
}

// scrubJob 抹掉编译任务 Error 字段里的 API Key：任务检查点里的失败原因来自模型服务商的原始响应，可能回显密钥；
// 它会被界面原样显示并允许复制，所以和其余错误文本一样在适配器出口统一脱敏。
func (a *wikiAdapter) scrubJob(j services.WikiCompileJob) services.WikiCompileJob {
	if j.Error != "" {
		j.Error = scrubKeys(j.Error, apiKeysOf(a.cfg.AISettings()))
	}
	return j
}

func wikiCall[T any](ctx context.Context, a *wikiAdapter, channel string, args ...any) (T, error) {
	out, err := callAs[T](ctx, a.b, channel, args...)
	return out, a.mapErr(err)
}

// ---- 目录与读取 ----

func (a *wikiAdapter) Catalog(ctx context.Context) ([]services.WikiCatalogEntry, error) {
	return wikiCall[[]services.WikiCatalogEntry](ctx, a, "wiki:catalog")
}

func (a *wikiAdapter) BacklinkCounts(ctx context.Context) (services.WikiBacklinkCounts, error) {
	return wikiCall[services.WikiBacklinkCounts](ctx, a, "wiki:backlinkCounts")
}

func (a *wikiAdapter) Status(ctx context.Context) (services.WikiStatus, error) {
	return wikiCall[services.WikiStatus](ctx, a, "wiki:status")
}

func (a *wikiAdapter) Search(ctx context.Context, query string, limit int) ([]services.WikiSearchHit, error) {
	return wikiCall[[]services.WikiSearchHit](ctx, a, "wiki:search", query, limit)
}

func (a *wikiAdapter) GetPage(ctx context.Context, id string) (*services.WikiPageDetail, error) {
	return wikiCall[*services.WikiPageDetail](ctx, a, "wiki:getPage", id)
}

func (a *wikiAdapter) ListRevisions(ctx context.Context, pageID string) ([]services.WikiRevision, error) {
	return wikiCall[[]services.WikiRevision](ctx, a, "wiki:listRevisions", pageID)
}

func (a *wikiAdapter) Graph(ctx context.Context) (services.WikiGraph, error) {
	return wikiCall[services.WikiGraph](ctx, a, "wiki:graph")
}

// ---- 写入 ----

func (a *wikiAdapter) UpdatePage(ctx context.Context, in services.WikiUpdatePageInput) (bool, error) {
	return wikiCall[bool](ctx, a, "wiki:updatePage", in)
}

func (a *wikiAdapter) DeletePage(ctx context.Context, id string) (bool, error) {
	return wikiCall[bool](ctx, a, "wiki:deletePage", id)
}

func (a *wikiAdapter) RestoreRevision(ctx context.Context, revisionID string) (bool, error) {
	return wikiCall[bool](ctx, a, "wiki:restoreRevision", revisionID)
}

// ---- 全文编译 ----

// wikiCompilerReply 是 wiki:compiler 各 action 的统一回执；Preview 留作原始 JSON，由调用方按需要的类型解码
// （驱动器用 aiflow 的类型，界面用 services 的类型，两者字段相同但计数的形态不同）。
type wikiCompilerReply struct {
	OK         bool                     `json:"ok"`
	Error      string                   `json:"error"`
	Preview    json.RawMessage          `json:"preview"`
	Job        *aiflow.WikiCompileJob   `json:"job"`
	Jobs       []aiflow.WikiCompileJob  `json:"jobs"`
	Work       *aiflow.WikiCompileWork  `json:"work"`
	Suggestion *services.WikiSuggestion `json:"suggestion"`
}

func compilerDo(ctx context.Context, b *Bridge, input map[string]any) (wikiCompilerReply, error) {
	return callAs[wikiCompilerReply](ctx, b, "wiki:compiler", input)
}

// decodePreview 解码回执里的预览；没有预览（null / 缺省）返回 false。
func decodePreview[T any](raw json.RawMessage) (*T, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("解码 wiki:compiler 的预览失败：%w", err)
	}
	return &out, nil
}

func (a *wikiAdapter) CompileConfigured(ctx context.Context) (bool, error) {
	return a.driver.ModelIdentity() != "", nil
}

func (a *wikiAdapter) CompilePreview(ctx context.Context) (services.WikiCompilePreview, error) {
	reply, err := compilerDo(ctx, a.b, map[string]any{"action": "preview", "model": a.driver.ModelIdentity()})
	if err != nil {
		return services.WikiCompilePreview{}, a.mapErr(err)
	}
	if !reply.OK {
		return services.WikiCompilePreview{}, errors.New(orStr(reply.Error, "编译预览失败"))
	}
	preview, err := decodePreview[services.WikiCompilePreview](reply.Preview)
	if err != nil {
		return services.WikiCompilePreview{}, err
	}
	if preview == nil {
		return services.WikiCompilePreview{}, errors.New("编译预览失败")
	}
	if preview.Entries == nil {
		preview.Entries = []services.WikiCompileCandidate{}
	}
	return *preview, nil
}

func (a *wikiAdapter) CompileJobs(ctx context.Context) ([]services.WikiCompileJob, error) {
	reply, err := compilerDo(ctx, a.b, map[string]any{"action": "jobs"})
	if err != nil {
		return nil, a.mapErr(err)
	}
	if !reply.OK {
		return nil, errors.New(orStr(reply.Error, "读取编译任务失败"))
	}
	out := make([]services.WikiCompileJob, len(reply.Jobs))
	for i, j := range reply.Jobs {
		out[i] = a.scrubJob(services.WikiCompileJob(j))
	}
	return out, nil
}

func (a *wikiAdapter) StartCompile(ctx context.Context, selected []services.WikiCompileCandidate) (services.WikiCompileJob, error) {
	model := a.driver.ModelIdentity()
	if model == "" {
		return services.WikiCompileJob{}, services.ErrAINotConfigured
	}
	if selected == nil {
		selected = []services.WikiCompileCandidate{}
	}
	reply, err := compilerDo(ctx, a.b, map[string]any{"action": "start", "model": model, "selected": selected, "allowUpgrade": true})
	if err != nil {
		return services.WikiCompileJob{}, a.mapErr(err)
	}
	if !reply.OK || reply.Job == nil {
		rejected := &services.WikiStartError{Message: scrubKeys(orStr(reply.Error, "任务未启动"), apiKeysOf(a.cfg.AISettings()))}
		if preview, err := decodePreview[services.WikiCompilePreview](reply.Preview); err == nil {
			rejected.Preview = preview
		}
		return services.WikiCompileJob{}, rejected
	}
	return a.scrubJob(services.WikiCompileJob(*reply.Job)), nil
}

func (a *wikiAdapter) RunCompile(ctx context.Context, job services.WikiCompileJob, progress func(services.WikiCompileJob)) (services.WikiCompileJob, error) {
	var onProgress func(aiflow.WikiCompileJob)
	if progress != nil {
		onProgress = func(j aiflow.WikiCompileJob) { progress(a.scrubJob(services.WikiCompileJob(j))) }
	}
	out, err := a.driver.RunJob(ctx, aiflow.WikiCompileJob(job), onProgress)
	return a.scrubJob(services.WikiCompileJob(out)), a.mapErr(err)
}

func (a *wikiAdapter) ControlCompile(ctx context.Context, jobID, status string) (services.WikiCompileJob, error) {
	job, err := a.driver.Control(ctx, jobID, status)
	if err != nil {
		return services.WikiCompileJob{}, a.mapErr(err)
	}
	if job == nil {
		return services.WikiCompileJob{}, errors.New("修改编译状态失败")
	}
	return a.scrubJob(services.WikiCompileJob(*job)), nil
}

// ---- 更新建议 ----

func (a *wikiAdapter) Suggestion(ctx context.Context, pageID string) (*services.WikiSuggestion, error) {
	reply, err := compilerDo(ctx, a.b, map[string]any{"action": "suggestion", "pageId": pageID})
	if err != nil {
		return nil, a.mapErr(err)
	}
	if !reply.OK {
		return nil, errors.New(orStr(reply.Error, "更新建议读取失败"))
	}
	return reply.Suggestion, nil
}

// AcceptSuggestion 令牌失效（建议或页面已变化）时处理器回 ok=false 与一句固定提示：这不是故障，按 false 返回，由界面重新读建议。
func (a *wikiAdapter) AcceptSuggestion(ctx context.Context, pageID, token string) (bool, error) {
	reply, err := compilerDo(ctx, a.b, map[string]any{"action": "suggestion", "pageId": pageID, "accept": true, "token": token})
	if err != nil {
		return false, a.mapErr(err)
	}
	return reply.OK, nil
}

// ---- aiflow.WikiStore：检查点读写 ----

// wikiCompilerStore 是 aiflow.WikiStore 的生产实现：每个方法对应渲染层的一次 wiki.compiler 调用。
// 调用本身失败（通道层出错）返回 error；业务失败（ok=false）经 WikiResult 交给驱动器。
type wikiCompilerStore struct{ b *Bridge }

var _ aiflow.WikiStore = wikiCompilerStore{}

func (s wikiCompilerStore) result(reply wikiCompilerReply) (aiflow.WikiResult, error) {
	res := aiflow.WikiResult{OK: reply.OK, Error: reply.Error, Job: reply.Job, Work: reply.Work}
	preview, err := decodePreview[aiflow.WikiCompilePreview](reply.Preview)
	if err != nil {
		return aiflow.WikiResult{}, err
	}
	res.Preview = preview
	return res, nil
}

func (s wikiCompilerStore) do(ctx context.Context, input map[string]any) (aiflow.WikiResult, error) {
	reply, err := compilerDo(ctx, s.b, input)
	if err != nil {
		return aiflow.WikiResult{}, err
	}
	return s.result(reply)
}

func (s wikiCompilerStore) Preview(ctx context.Context, model string, ids []string) (aiflow.WikiResult, error) {
	input := map[string]any{"action": "preview", "model": model}
	if ids != nil {
		input["ids"] = ids
	}
	return s.do(ctx, input)
}

func (s wikiCompilerStore) Start(ctx context.Context, model string, selected []aiflow.WikiCandidate) (aiflow.WikiResult, error) {
	return s.do(ctx, map[string]any{"action": "start", "model": model, "selected": selected, "allowUpgrade": true})
}

func (s wikiCompilerStore) Next(ctx context.Context, jobID string) (aiflow.WikiResult, error) {
	return s.do(ctx, map[string]any{"action": "next", "id": jobID})
}

func (s wikiCompilerStore) Finish(ctx context.Context, work aiflow.WikiCompileWork, contributions []aiflow.WikiContribution, errMsg *string) (aiflow.WikiResult, error) {
	input := map[string]any{"action": "finish", "work": work}
	if errMsg != nil {
		input["error"] = *errMsg
	} else {
		if contributions == nil {
			contributions = []aiflow.WikiContribution{}
		}
		input["contributions"] = contributions
	}
	return s.do(ctx, input)
}

func (s wikiCompilerStore) Control(ctx context.Context, jobID, status string) (aiflow.WikiResult, error) {
	return s.do(ctx, map[string]any{"action": "control", "id": jobID, "status": status})
}

func (s wikiCompilerStore) Catalog(ctx context.Context) ([]aiflow.WikiCatalogEntry, error) {
	return callAs[[]aiflow.WikiCatalogEntry](ctx, s.b, "wiki:catalog")
}
