package imports

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 对应渲染层 ImportTaskDetailModal 的 retryAuthenticated、NodeseekRetryButton 与 import-task-meta 的 nodeseekVerificationTarget。

type retryCall struct {
	id   string
	opts services.ImportRetryOptions
}

// fakeRetrier 记录重试请求，结果可编排。
type fakeRetrier struct {
	mu     sync.Mutex
	calls  []retryCall
	result bool
}

func (f *fakeRetrier) RetryTask(_ context.Context, id string, opts services.ImportRetryOptions) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, retryCall{id, opts})
	return f.result
}

func (f *fakeRetrier) got() []retryCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]retryCall(nil), f.calls...)
}

type authHarness struct {
	auth   *servicestest.PlatformAuth
	retry  *fakeRetrier
	ar     *AuthRetry
	log    *recLog
	notify *kit.Notifier
}

func newAuthHarness(statuses ...services.PlatformStatus) *authHarness {
	h := &authHarness{auth: &servicestest.PlatformAuth{}, retry: &fakeRetrier{result: true}, log: &recLog{}, notify: kit.NewNotifier(nil)}
	h.auth.SetStatuses(statuses...)
	h.ar = &AuthRetry{Auth: h.auth, Retry: h.retry, Guard: kit.Guard{Notify: h.notify, Log: h.log}}
	return h
}

func xhsTask() services.ImportTask {
	return services.ImportTask{ID: "t1", SourceInput: "https://www.xiaohongshu.com/explore/1", CaptureStrategy: "default"}
}

func xhsStatus(loggedIn bool) services.PlatformStatus {
	return services.PlatformStatus{Platform: PlatformXiaohongshu, Browser: "WebView2", BrowserVersion: "130.0", Available: true, LoggedIn: loggedIn}
}

// ---- 登录态重试 ----

func TestAuthRetryAlreadyLoggedIn(t *testing.T) {
	h := newAuthHarness(xhsStatus(true))
	if !h.ar.RetryAuthenticated(xhsTask(), PlatformXiaohongshu) {
		t.Fatal("已登录应直接重试成功")
	}
	if slices.Contains(h.auth.Calls(), "login:"+PlatformXiaohongshu) {
		t.Fatal("已登录不应再打开登录窗口")
	}
	want := []retryCall{{"t1", services.ImportRetryOptions{CaptureStrategy: "authenticated"}}}
	if got := h.retry.got(); !slices.Equal(got, want) {
		t.Fatalf("重试请求 = %+v，期望 %+v", got, want)
	}
	if len(h.notify.Queued()) != 0 {
		t.Fatal("成功不应弹提示")
	}
}

func TestAuthRetryLogsInWhenNeeded(t *testing.T) {
	h := newAuthHarness(xhsStatus(false))
	if !h.ar.RetryAuthenticated(xhsTask(), PlatformXiaohongshu) {
		t.Fatal("登录成功后应重试成功")
	}
	calls := h.auth.Calls()
	if !slices.Equal(calls, []string{"status", "login:" + PlatformXiaohongshu}) {
		t.Fatalf("调用顺序 = %v", calls)
	}
	if len(h.retry.got()) != 1 {
		t.Fatal("登录后应发起一次重试")
	}
}

func TestAuthRetryStopsWhenLoginFails(t *testing.T) {
	h := newAuthHarness(xhsStatus(false))
	h.auth.FailLogin(PlatformXiaohongshu, errors.New("window closed by user"))
	if h.ar.RetryAuthenticated(xhsTask(), PlatformXiaohongshu) {
		t.Fatal("登录失败不能报已发起")
	}
	if len(h.retry.got()) != 0 {
		t.Fatal("登录失败不应重试")
	}
	q := h.notify.Queued()
	if len(q) != 1 || q[0].Kind != kit.ToastError || q[0].Message != locales.ImportsAuthenticatedRetryFailed() || q[0].Detail != "window closed by user" {
		t.Fatalf("应弹一条标题固定、详情带原因的错误提示：%+v", q)
	}
	if e := h.log.entries(); len(e) != 1 || !strings.Contains(e[0], "window closed by user") {
		t.Fatalf("日志应带原因：%v", e)
	}
}

func TestAuthRetryBrowserUnavailable(t *testing.T) {
	cases := map[string][]services.PlatformStatus{
		"浏览器不可用":  {{Platform: PlatformXiaohongshu, Available: false}},
		"平台不在列表里": {{Platform: PlatformDouyin, Available: true}},
		"没有任何平台":  nil,
	}
	for name, statuses := range cases {
		t.Run(name, func(t *testing.T) {
			h := newAuthHarness(statuses...)
			if h.ar.RetryAuthenticated(xhsTask(), PlatformXiaohongshu) {
				t.Fatal("不可用不能报已发起")
			}
			if slices.Contains(h.auth.Calls(), "login:"+PlatformXiaohongshu) || len(h.retry.got()) != 0 {
				t.Fatal("不可用时不应登录也不应重试")
			}
			if e := h.log.entries(); len(e) != 1 || !strings.Contains(e[0], locales.SettingsPlatformBrowserUnavailable()) {
				t.Fatalf("应报告浏览器不可用：%v", e)
			}
		})
	}
}

func TestAuthRetryStatusError(t *testing.T) {
	h := newAuthHarness()
	h.auth.FailStatuses(errors.New("status unavailable"))
	if h.ar.RetryAuthenticated(xhsTask(), PlatformXiaohongshu) {
		t.Fatal("读状态失败不能报已发起")
	}
	if e := h.log.entries(); len(e) != 1 || !strings.Contains(e[0], "status unavailable") || len(h.notify.Queued()) != 1 {
		t.Fatalf("应带原因提示：%v", e)
	}
}

func TestAuthRetryNoAuthService(t *testing.T) {
	h := newAuthHarness()
	h.ar.Auth = nil
	if h.ar.RetryAuthenticated(xhsTask(), PlatformXiaohongshu) || len(h.notify.Queued()) != 1 {
		t.Fatal("没有登录服务应提示不可用")
	}
	if err := h.ar.VerifyNodeseek(context.Background(), xhsTask()); err == nil {
		t.Fatal("没有登录服务时验证应报错")
	}
	if err := h.ar.CancelNodeseek(); err != nil {
		t.Fatalf("没有登录服务时取消不算失败：%v", err)
	}
	if h.ar.CaptureBrowser(PlatformXiaohongshu) != "" {
		t.Fatal("没有登录服务时浏览器信息为空")
	}
}

func TestAuthRetryReturnsRetryResult(t *testing.T) {
	h := newAuthHarness(xhsStatus(true))
	h.retry.result = false // 重试失败由 RetryTask 自己提示；这里只透传「没发起」
	if h.ar.RetryAuthenticated(xhsTask(), PlatformXiaohongshu) {
		t.Fatal("重试没发起应返回 false")
	}
	if len(h.notify.Queued()) != 0 {
		t.Fatal("重试失败的提示归 RetryTask，这里不重复弹")
	}
}

// 同一任务连点：登录窗口还在等用户时，第二次点击不能再拉起第二个流程；结束后可以再来。
func TestAuthRetryIgnoresDuplicateWhileInFlight(t *testing.T) {
	h := newAuthHarness(xhsStatus(false))
	finish := h.auth.HoldLogin(PlatformXiaohongshu)
	first := make(chan bool, 1)
	go func() { first <- h.ar.RetryAuthenticated(xhsTask(), PlatformXiaohongshu) }()
	waitFor(t, "登录窗口已打开", func() bool { return h.auth.Pending(PlatformXiaohongshu) })

	if h.ar.RetryAuthenticated(xhsTask(), PlatformXiaohongshu) {
		t.Fatal("流程进行中的重复点击应被忽略")
	}
	other := xhsTask()
	other.ID = "t2"
	otherDone := make(chan bool, 1)
	finishOther := h.auth.HoldLogin(PlatformXiaohongshu)
	go func() { otherDone <- h.ar.RetryAuthenticated(other, PlatformXiaohongshu) }()
	waitFor(t, "另一个任务的流程已开始", func() bool { return len(h.auth.LoginOptions()) == 2 })
	if n := len(h.auth.LoginOptions()); n != 2 {
		t.Fatalf("不同任务互不影响，登录次数 = %d", n)
	}

	finish(nil)
	if !<-first {
		t.Fatal("登录完成后第一个流程应发起重试")
	}
	finishOther(nil)
	<-otherDone
	if n := len(h.retry.got()); n != 2 {
		t.Fatalf("两个任务各重试一次，得到 %d", n)
	}
	if !h.ar.RetryAuthenticated(xhsTask(), PlatformXiaohongshu) {
		t.Fatal("流程结束后应能再次发起")
	}
}

// ---- NodeSeek 验证 ----

const nodeseekPost = "https://www.nodeseek.com/post-123-1"

func blockedTask(source, errText string) services.ImportTask {
	return services.ImportTask{ID: "n1", SourceInput: source, Error: errText}
}

func TestVerifyNodeseekUsesBlockedPageAndCancels(t *testing.T) {
	h := newAuthHarness(services.PlatformStatus{Platform: PlatformNodeseek, Available: true})
	blocked := "https://www.nodeseek.com/post-123-3"
	task := blockedTask(nodeseekPost, "nodeseek_verification_required: "+blocked)

	finish := h.auth.HoldLogin(PlatformNodeseek)
	done := make(chan error, 1)
	go func() { done <- h.ar.VerifyNodeseek(context.Background(), task) }()
	waitFor(t, "验证窗口已打开", func() bool { return h.auth.Pending(PlatformNodeseek) })

	if opts := h.auth.LoginOptions(); len(opts) != 1 || opts[0].VerificationURL != blocked {
		t.Fatalf("应带受阻分页地址打开验证窗口：%+v", opts)
	}
	if err := h.ar.CancelNodeseek(); err != nil {
		t.Fatalf("取消：%v", err)
	}
	if err := <-done; err == nil {
		t.Fatal("取消后验证应返回错误，由调用方静默处理")
	}
	finish(nil) // 已结束的登录再 finish 不应有影响
	if len(h.retry.got()) != 0 {
		t.Fatal("验证本身不重试，重试由界面在验证通过后发起")
	}
}

func TestVerifyNodeseekSuccess(t *testing.T) {
	h := newAuthHarness()
	if err := h.ar.VerifyNodeseek(context.Background(), blockedTask(nodeseekPost, "")); err != nil {
		t.Fatal(err)
	}
	if opts := h.auth.LoginOptions(); len(opts) != 1 || opts[0].VerificationURL != nodeseekPost {
		t.Fatalf("没有受阻记录时用来源地址：%+v", opts)
	}
}

func TestVerifyNodeseekFailureAndContextCancel(t *testing.T) {
	h := newAuthHarness()
	h.auth.FailLogin(PlatformNodeseek, errors.New("verification timed out"))
	if err := h.ar.VerifyNodeseek(context.Background(), blockedTask(nodeseekPost, "")); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("应透传失败原因：%v", err)
	}
	if len(h.notify.Queued()) != 0 {
		t.Fatal("验证失败的提示由调用方决定（取消时静默），这里不弹")
	}

	h2 := newAuthHarness()
	h2.auth.HoldLogin(PlatformNodeseek)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h2.ar.VerifyNodeseek(ctx, blockedTask(nodeseekPost, "")) }()
	waitFor(t, "验证窗口已打开", func() bool { return h2.auth.Pending(PlatformNodeseek) })
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("上下文取消应返回取消错误：%v", err)
	}
}

func TestCancelNodeseek(t *testing.T) {
	h := newAuthHarness()
	if err := h.ar.CancelNodeseek(); err != nil {
		t.Fatalf("没有进行中的验证不算失败：%v", err)
	}
	h.auth.FailCancel(errors.New("cancel refused"))
	if err := h.ar.CancelNodeseek(); err == nil || !strings.Contains(err.Error(), "cancel refused") {
		t.Fatalf("取消失败应透传原因：%v", err)
	}
}

func TestNodeseekVerificationTarget(t *testing.T) {
	blocked := "https://www.nodeseek.com/post-123-3"
	otherTopic := "https://www.nodeseek.com/post-999-2"
	cases := []struct {
		name, source, err, want string
	}{
		{"同一帖子的受阻分页", nodeseekPost, "nodeseek_verification_required " + blocked, blocked},
		{"取第一个同帖子的候选", nodeseekPost, "nodeseek_verification_required " + otherTopic + " " + blocked + " https://www.nodeseek.com/post-123-9", blocked},
		{"别的帖子不复用", nodeseekPost, "nodeseek_verification_required " + otherTopic, nodeseekPost},
		{"错误里没有验证标记不复用", nodeseekPost, "fetch failed " + blocked, nodeseekPost},
		{"错误为空", nodeseekPost, "", nodeseekPost},
		{"来源不是 NodeSeek 帖子页", "https://www.nodeseek.com/", "nodeseek_verification_required " + blocked, "https://www.nodeseek.com/"},
		{"来源是别的站点", "https://example.com/post-123-1", "nodeseek_verification_required " + blocked, "https://example.com/post-123-1"},
		{"来源不是网址", "plain text", "nodeseek_verification_required " + blocked, "plain text"},
		{"裸域名也认", "https://nodeseek.com/post-123-1", "nodeseek_verification_required " + blocked, blocked},
		{"帖子 id 不能以 0 开头", "https://www.nodeseek.com/post-0123-1", "nodeseek_verification_required " + blocked, "https://www.nodeseek.com/post-0123-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NodeseekVerificationTarget(tc.source, tc.err); got != tc.want {
				t.Fatalf("NodeseekVerificationTarget(%q, %q) = %q，期望 %q", tc.source, tc.err, got, tc.want)
			}
		})
	}
}

// ---- 诊断信息里的浏览器 ----

func TestCaptureBrowser(t *testing.T) {
	h := newAuthHarness(
		xhsStatus(true),
		services.PlatformStatus{Platform: PlatformDouyin, Browser: "WebView2", Available: true},
		services.PlatformStatus{Platform: PlatformLinuxdo, Available: true},
	)
	if got := h.ar.CaptureBrowser(PlatformXiaohongshu); got != "WebView2 130.0" {
		t.Fatalf("名称加版本，得到 %q", got)
	}
	if got := h.ar.CaptureBrowser(PlatformDouyin); got != "WebView2" {
		t.Fatalf("没有版本只写名称，得到 %q", got)
	}
	if got := h.ar.CaptureBrowser(PlatformLinuxdo); got != "" {
		t.Fatalf("没有浏览器信息应为空，得到 %q", got)
	}
	if got := h.ar.CaptureBrowser("unknown"); got != "" {
		t.Fatalf("平台不在列表里应为空，得到 %q", got)
	}
	h.auth.FailStatuses(errors.New("boom"))
	if got := h.ar.CaptureBrowser(PlatformXiaohongshu); got != "" {
		t.Fatalf("读状态失败应为空（诊断少一行），得到 %q", got)
	}
	if len(h.notify.Queued()) != 0 || len(h.log.entries()) != 0 {
		t.Fatal("读不到浏览器信息不算失败，不弹提示也不记日志")
	}
}

func TestInstallWiresAllHooks(t *testing.T) {
	h := newAuthHarness()
	var hooks Hooks
	h.ar.Install(&hooks)
	if hooks.RetryAuthenticated == nil || hooks.VerifyNodeseek == nil || hooks.CancelNodeseek == nil || hooks.CaptureBrowser == nil {
		t.Fatalf("四个钩子都应装上：%+v", hooks)
	}
}
