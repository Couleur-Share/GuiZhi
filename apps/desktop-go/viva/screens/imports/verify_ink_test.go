package imports

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// 台账 C-008 / B-058 在导入详情的接线：验证进行中主按钮变成禁用的「等待网页验证…」（verifyStyle，N = 0.5），
// 文字按整层淡出取色，面板是详情弹窗 UiPanelStrongBg；验证结束后恢复原色。期望值由「文字叠在面板上、不透明度 N」独立算出。
// 浅色下弹窗面板与页面背景都是白的，接错面板令牌看不出来，所以再用深色外观跑一遍。
func TestVerifyingButtonInkFollowsTheDetailPanel(t *testing.T) {
	for _, mode := range []struct {
		name string
		dark bool
	}{{"浅色", false}, {"深色", true}} {
		t.Run(mode.name, func(t *testing.T) {
			release := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			started := make(chan struct{}, 1)
			r := openedDetail(t, nodeseekTask(), func(r *rig) {
				if !mode.dark {
					return
				}
				spec := kit.DefaultSpec()
				spec.Motion, spec.Mode = kit.MotionLevelOff, tokens.ModeDark
				r.screen.Look = kit.NewLook(spec)
				r.screen.Look.Scale, r.screen.Look.Viewport = 1, 1280
			})
			r.screen.Hooks.VerifyNodeseek = func(_ context.Context, _ services.ImportTask) error {
				started <- struct{}{}
				<-release // 等用户在验证窗口里操作；被取消时返回
				return errors.New("验证已取消")
			}
			r.screen.Hooks.CancelNodeseek = func() error { unblock(); return nil }
			r.store.Spawn = func(fn func()) { go fn() }

			look := r.screen.Look
			tk := look.T
			ink := func(text string) (gfx.Color, bool) {
				for _, tx := range r.session.Snapshot().Texts {
					if tx.Value == text {
						return tx.Style.Color, true
					}
				}
				return gfx.Color{}, false
			}
			idle := func() bool {
				r.frames(1)
				return r.has(gfx.RoleButton, locales.ImportsNodeseekVerifyRetry()) && !r.has(gfx.RoleButton, locales.CommonCancel())
			}

			if got, ok := ink(locales.ImportsNodeseekVerifyRetry()); !ok || got != look.Col(tk.PrimaryForeground) {
				t.Fatalf("验证之前的文字色 %+v（找到=%v），期望原色 %+v", got, ok, look.Col(tk.PrimaryForeground))
			}
			r.click(gfx.RoleButton, locales.ImportsNodeseekVerifyRetry())
			<-started
			r.frames(2)
			if n, ok := r.node(gfx.RoleButton, locales.ImportsNodeseekVerifying()); !ok || !n.Disabled {
				t.Fatalf("等待验证时主按钮应写「等待网页验证…」并禁用：%+v ok=%v", n, ok)
			}
			got, ok := ink(locales.ImportsNodeseekVerifying())
			if want := look.Col(tk.PrimaryForeground.WithAlpha(0.5).Over(tk.UiPanelStrongBg)); !ok || got != want {
				t.Errorf("禁用的「等待网页验证…」文字色 %+v（找到=%v），期望整层淡出的合成色 %+v", got, ok, want)
			}

			r.click(gfx.RoleButton, locales.CommonCancel())
			waitFor(t, "验证结束、按钮恢复", idle)
			if got, ok := ink(locales.ImportsNodeseekVerifyRetry()); !ok || got != look.Col(tk.PrimaryForeground) {
				t.Errorf("验证结束后的文字色 %+v（找到=%v），期望原色 %+v", got, ok, look.Col(tk.PrimaryForeground))
			}
		})
	}
}
