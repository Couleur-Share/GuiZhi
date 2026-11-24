package imports

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"

	whatwg "github.com/nlnwa/whatwg-url/url"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 登录态重试与站点验证的协调（渲染层 ImportTaskDetailModal 的 retryAuthenticated、NodeseekRetryButton、诊断信息里的浏览器行）：
// 把 imports.Hooks 的四个回调 RetryAuthenticated、VerifyNodeseek、CancelNodeseek、CaptureBrowser 接到 services.PlatformAuth 与队列 Store 上。
// 界面（详情弹窗的按钮、等待态、取消）早就按这四个钩子写好了，这里补的是钩子背后的流程。

// TaskRetrier 是「重试一个任务」的能力（Store 满足）：失败由它自己弹带原因的提示，只返回是否已发起。
type TaskRetrier interface {
	RetryTask(ctx context.Context, id string, opts services.ImportRetryOptions) bool
}

// AuthRetry 协调登录态重试与 NodeSeek 验证。零值之外只需要填 Auth 与 Retry；Ctx 是应用生命周期（取消验证与探测浏览器时用）。
type AuthRetry struct {
	Auth  services.PlatformAuth
	Retry TaskRetrier
	Guard kit.Guard
	Ctx   context.Context

	mu       sync.Mutex
	inflight map[string]bool // 正在走登录态重试流程的任务 id：连点按钮不能同时拉起两个登录窗口
}

// Install 把四个钩子装到 Hooks 上（其余钩子不动）。
func (a *AuthRetry) Install(h *Hooks) {
	h.RetryAuthenticated = a.RetryAuthenticated
	h.VerifyNodeseek = a.VerifyNodeseek
	h.CancelNodeseek = a.CancelNodeseek
	h.CaptureBrowser = a.CaptureBrowser
}

func (a *AuthRetry) ctx() context.Context {
	if a.Ctx != nil {
		return a.Ctx
	}
	return context.Background()
}

// RetryAuthenticated 用内置登录窗口的登录态重试一个任务：先查平台状态（登录窗口不可用就报错）、
// 没登录就打开登录窗口等用户登录，再以 authenticated 策略重试。会阻塞（等用户登录），在后台调用。
//
// 返回「已发起重试」（详情弹窗据此关闭）。前置步骤失败弹「登录态重试未开始」并带原因；重试本身失败由 RetryTask 提示。
// 同一任务的流程还在进行时再点不会重复拉起窗口，直接返回 false、不提示。
func (a *AuthRetry) RetryAuthenticated(task services.ImportTask, platform string) bool {
	a.mu.Lock()
	if a.inflight[task.ID] {
		a.mu.Unlock()
		return false
	}
	if a.inflight == nil {
		a.inflight = map[string]bool{}
	}
	a.inflight[task.ID] = true
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.inflight, task.ID)
		a.mu.Unlock()
	}()

	ctx := a.ctx()
	if err := a.ensureLoggedIn(ctx, platform); err != nil {
		a.Guard.Fail(locales.ImportsAuthenticatedRetryFailed(), "imports.authenticatedRetry", err)
		return false
	}
	if a.Retry == nil {
		return false
	}
	return a.Retry.RetryTask(ctx, task.ID, services.ImportRetryOptions{CaptureStrategy: "authenticated"})
}

// ensureLoggedIn 确认平台的内置登录窗口可用且已登录；没登录就打开登录窗口（阻塞到用户完成或失败）。
func (a *AuthRetry) ensureLoggedIn(ctx context.Context, platform string) error {
	if a.Auth == nil {
		return errors.New(locales.SettingsPlatformBrowserUnavailable())
	}
	list, err := a.Auth.Statuses(ctx)
	if err != nil {
		return err
	}
	status, found := services.FindPlatformStatus(list, platform)
	if !found || !status.Available {
		return errors.New(locales.SettingsPlatformBrowserUnavailable())
	}
	if status.LoggedIn {
		return nil
	}
	_, err = a.Auth.Login(ctx, platform, services.PlatformLoginOptions{})
	return err
}

// VerifyNodeseek 打开 NodeSeek 的验证窗口并等用户通过验证。每次都带受阻帖子的地址：旧的「已验证」标记可能已过期，以目标帖子实际可读为准。
// 取消、窗口被关或超时返回错误（由调用方决定提示还是静默）；通过后由界面带登录态重试任务。
func (a *AuthRetry) VerifyNodeseek(ctx context.Context, task services.ImportTask) error {
	if a.Auth == nil {
		return errors.New(locales.SettingsPlatformBrowserUnavailable())
	}
	_, err := a.Auth.Login(ctx, PlatformNodeseek, services.PlatformLoginOptions{
		VerificationURL: NodeseekVerificationTarget(task.SourceInput, task.Error),
	})
	return err
}

// CancelNodeseek 取消正在等待的 NodeSeek 验证；没有进行中的验证不算失败。
func (a *AuthRetry) CancelNodeseek() error {
	if a.Auth == nil {
		return nil
	}
	_, err := a.Auth.CancelLogin(a.ctx(), PlatformNodeseek)
	return err
}

// CaptureBrowser 是登录态采集用的浏览器（名称加版本，复制诊断信息用）；读不到状态或没有浏览器信息返回空串（诊断文本少一行，不算失败）。
func (a *AuthRetry) CaptureBrowser(platform string) string {
	if a.Auth == nil {
		return ""
	}
	list, err := a.Auth.Statuses(a.ctx())
	if err != nil {
		return ""
	}
	status, found := services.FindPlatformStatus(list, platform)
	if !found || status.Browser == "" {
		return ""
	}
	if status.BrowserVersion != "" {
		return status.Browser + " " + status.BrowserVersion
	}
	return status.Browser
}

var (
	// 帖子页路径：/post-{topicId}-{page}（渲染层 detectForumPlatform 对 NodeSeek 的判定）。
	nodeseekPostPath = regexp.MustCompile(`^/post-([1-9][0-9]*)-[1-9][0-9]*/?$`)
	// 错误文本里记录的受阻分页地址。
	nodeseekBlockedURL = regexp.MustCompile(`https://www\.nodeseek\.com/post-[1-9][0-9]*-[1-9][0-9]*`)
)

// nodeseekTopicID 取 NodeSeek 帖子地址里的帖子 id；不是 NodeSeek 的帖子页返回空串。
func nodeseekTopicID(raw string) string {
	u, err := whatwg.Parse(raw)
	if err != nil {
		return ""
	}
	if host := strings.ToLower(u.Hostname()); host != "nodeseek.com" && host != "www.nodeseek.com" {
		return ""
	}
	if m := nodeseekPostPath.FindStringSubmatch(u.Pathname()); m != nil {
		return m[1]
	}
	return ""
}

// NodeseekVerificationTarget 求验证窗口要打开的地址（渲染层 nodeseekVerificationTarget）：
// 只复用「同一帖子在错误里明确记录的受阻分页地址」，否则用任务的来源地址。
func NodeseekVerificationTarget(source, errText string) string {
	topic := nodeseekTopicID(source)
	if topic == "" || !strings.Contains(errText, "nodeseek_verification_required") {
		return source
	}
	for _, candidate := range nodeseekBlockedURL.FindAllString(errText, -1) {
		if nodeseekTopicID(candidate) == topic {
			return candidate
		}
	}
	return source
}
