package main

// 采集模块收尾（台账 B-013、B-015）的两个适配器：文档站批次与登录态重试用的平台账号能力。
//
// 与 native_adapters.go 的导入队列适配器同一做法：经 Bridge.call 调用已登记的处理器，
// 行为只有一个来源（渲染进程用的 crawl:* 与 platformCapture:* 处理器），这里只做 JSON 形状的翻译：
//
//	DocSite.CreateJob / ListJobs / GetJob       → crawl:create / crawl:list / crawl:get
//	DocSite.PauseJob / ResumeJob / RetryJob / CancelJob → crawl:pause / crawl:resume / crawl:retry / crawl:cancel
//	PlatformAuth.Statuses / Login / CancelLogin → platformCapture:status / platformCapture:login / platformCapture:cancelLogin
//
// 网络安全不在这里：入口网址的规范化、同源与目录范围、robots 与 SSRF 防护都在处理器与底层 safe_fetch 里，
// 适配层不发任何网络请求；错误文本只转述处理器给出的原因，不拼接网址以外的内容，不涉及 Cookie 或令牌。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"guizhi-mygo-shell/native/services"
)

// crawlEnvelope 是 crawl:* 处理器的统一回包：成功 { ok:true, data }，失败 { ok:false, error }。
type crawlEnvelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error string          `json:"error"`
}

// crawlCall 调用一个 crawl:* 处理器并解开信封：{ ok:false } 变成带原因的 error，成功返回 data（可能为空）。
func crawlCall(ctx context.Context, b *Bridge, channel string, args ...any) (json.RawMessage, error) {
	raw, err := b.call(ctx, channel, args...)
	if err != nil {
		return nil, err
	}
	var env crawlEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("解码 %s 的结果失败：%w", channel, err)
	}
	if !env.OK {
		if env.Error == "" {
			return nil, fmt.Errorf("%s 失败（处理器没有给出原因）", channel)
		}
		return nil, errors.New(env.Error)
	}
	return env.Data, nil
}

// wireCrawlInput 是批次输入在线路上的形状：collectionId 在处理器里原样透传（可能是 null、字符串，旧库里理论上还可能是别的），
// 单独解成原始值，非字符串一律当「未归合集」，免得一条异常的旧记录让整个批次列表解不出来。
type wireCrawlInput struct {
	Purpose         string               `json:"purpose"`
	Seeds           []services.CrawlSeed `json:"seeds"`
	CollectionID    json.RawMessage      `json:"collectionId"`
	MaxPages        int                  `json:"maxPages"`
	MaxDepth        int                  `json:"maxDepth"`
	DuplicatePolicy string               `json:"duplicatePolicy"`
}

type wireCrawlJob struct {
	ID        string         `json:"id"`
	Input     wireCrawlInput `json:"input"`
	Status    string         `json:"status"`
	CreatedAt int64          `json:"createdAt"`
	UpdatedAt int64          `json:"updatedAt"`
	Error     *string        `json:"error"`
	Counts    map[string]int `json:"counts"`
}

func (w wireCrawlJob) toService() services.CrawlJob {
	job := services.CrawlJob{
		ID: w.ID, Status: w.Status, CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt, Counts: w.Counts,
		Input: services.CrawlInput{
			Purpose: w.Input.Purpose, Seeds: w.Input.Seeds, MaxPages: w.Input.MaxPages,
			MaxDepth: w.Input.MaxDepth, DuplicatePolicy: w.Input.DuplicatePolicy,
		},
	}
	var collection string
	if json.Unmarshal(w.Input.CollectionID, &collection) == nil && collection != "" {
		job.Input.CollectionID = &collection
	}
	if w.Error != nil {
		job.Error = *w.Error
	}
	if job.Counts == nil {
		job.Counts = map[string]int{}
	}
	return job
}

// docSiteAdapter 把 crawl:* 处理器接成 services.DocSite。
type docSiteAdapter struct{ b *Bridge }

var _ services.DocSite = docSiteAdapter{}

func (a docSiteAdapter) CreateJob(ctx context.Context, in services.CrawlInput) (string, error) {
	data, err := crawlCall(ctx, a.b, "crawl:create", in)
	if err != nil {
		return "", err
	}
	var id string
	if err := json.Unmarshal(data, &id); err != nil || id == "" {
		return "", errors.New("crawl:create 没有返回批次 id")
	}
	return id, nil
}

func (a docSiteAdapter) ListJobs(ctx context.Context) ([]services.CrawlJob, error) {
	data, err := crawlCall(ctx, a.b, "crawl:list")
	if err != nil {
		return nil, err
	}
	var wire []wireCrawlJob
	if len(data) > 0 {
		if err := json.Unmarshal(data, &wire); err != nil {
			return nil, fmt.Errorf("解码 crawl:list 的结果失败：%w", err)
		}
	}
	jobs := make([]services.CrawlJob, len(wire))
	for i, w := range wire {
		jobs[i] = w.toService()
	}
	return jobs, nil
}

func (a docSiteAdapter) GetJob(ctx context.Context, id string) (services.CrawlJobDetail, error) {
	data, err := crawlCall(ctx, a.b, "crawl:get", id)
	if err != nil {
		return services.CrawlJobDetail{}, err
	}
	var wire struct {
		Job   *wireCrawlJob        `json:"job"`
		Pages []services.CrawlPage `json:"pages"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return services.CrawlJobDetail{}, fmt.Errorf("解码 crawl:get 的结果失败：%w", err)
	}
	detail := services.CrawlJobDetail{Pages: wire.Pages}
	if detail.Pages == nil {
		detail.Pages = []services.CrawlPage{}
	}
	if wire.Job != nil {
		job := wire.Job.toService()
		detail.Job = &job
	}
	return detail, nil
}

func (a docSiteAdapter) PauseJob(ctx context.Context, id string) error {
	_, err := crawlCall(ctx, a.b, "crawl:pause", id)
	return err
}

func (a docSiteAdapter) ResumeJob(ctx context.Context, id string) error {
	_, err := crawlCall(ctx, a.b, "crawl:resume", id)
	return err
}

func (a docSiteAdapter) RetryJob(ctx context.Context, id string) error {
	_, err := crawlCall(ctx, a.b, "crawl:retry", id)
	return err
}

func (a docSiteAdapter) CancelJob(ctx context.Context, id string) error {
	_, err := crawlCall(ctx, a.b, "crawl:cancel", id)
	return err
}

// platformAuthAdapter 把 platformCapture:* 处理器接成 services.PlatformAuth。
type platformAuthAdapter struct{ b *Bridge }

var _ services.PlatformAuth = platformAuthAdapter{}

func (a platformAuthAdapter) Statuses(ctx context.Context) ([]services.PlatformStatus, error) {
	return callAs[[]services.PlatformStatus](ctx, a.b, "platformCapture:status")
}

// Login 的请求形状与渲染层 platformCapture.login 一致：{ platform, forceRelogin, verificationUrl? }；
// verificationUrl 只在非空时带上（处理器只对 nodeseek 采用，并会再校验它是 NodeSeek 的帖子页）。
func (a platformAuthAdapter) Login(ctx context.Context, platform string, opts services.PlatformLoginOptions) (services.PlatformStatus, error) {
	arg := map[string]any{"platform": platform, "forceRelogin": opts.ForceRelogin}
	if opts.VerificationURL != "" {
		arg["verificationUrl"] = opts.VerificationURL
	}
	return callAs[services.PlatformStatus](ctx, a.b, "platformCapture:login", arg)
}

func (a platformAuthAdapter) CancelLogin(ctx context.Context, platform string) (bool, error) {
	return callAs[bool](ctx, a.b, "platformCapture:cancelLogin", platform)
}
