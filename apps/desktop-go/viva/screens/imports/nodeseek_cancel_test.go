package imports

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 取消赢过迟到的验证成功：用户点「取消」的同时，验证窗口恰好通过了验证（钩子返回 nil 而不是错误）。
// 取消之后既不带登录态重试任务、也不弹失败；真实的 platformCapture.login 在取消与通过几乎同时发生时就会这样返回。
func TestDetailNodeseekCancelWinsOverLateSuccess(t *testing.T) {
	var mu sync.Mutex
	var log []string
	record := func(s string) {
		mu.Lock()
		log = append(log, s)
		mu.Unlock()
	}
	release := make(chan struct{})
	r := openedDetail(t, nodeseekTask())
	r.screen.Hooks.VerifyNodeseek = func(_ context.Context, task services.ImportTask) error {
		record("verify:" + task.ID)
		<-release
		return nil // 窗口在取消生效前一刻通过了验证
	}
	r.screen.Hooks.CancelNodeseek = func() error {
		record("cancel")
		close(release)
		return nil
	}
	r.store.Spawn = func(fn func()) { go fn() }

	r.click(gfx.RoleButton, locales.ImportsNodeseekVerifyRetry())
	r.click(gfx.RoleButton, locales.CommonCancel())
	waitFor(t, "验证结束、按钮恢复", func() bool {
		r.frames(1)
		return r.has(gfx.RoleButton, locales.ImportsNodeseekVerifyRetry()) && !r.has(gfx.RoleButton, locales.CommonCancel())
	})

	mu.Lock()
	got := slices.Clone(log)
	mu.Unlock()
	if !slices.Equal(got, []string{"verify:task-1", "cancel"}) {
		t.Errorf("应先开验证窗口、再收到取消，实际 %v", got)
	}
	if len(r.calls()) != 0 {
		t.Errorf("取消之后验证即使通过也不应重试任务：%v", r.calls())
	}
	if r.toastShown(locales.ImportsNodeseekVerificationFailed()) {
		t.Errorf("取消之后不应弹验证失败；画面上有 %q", r.session.Texts())
	}
	if !r.dialogOpen("帖子") {
		t.Error("取消验证不应关闭弹窗")
	}
}
