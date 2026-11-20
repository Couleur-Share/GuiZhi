package settings_test

import (
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"

	nsettings "guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	sscreen "guizhi-mygo-shell/viva/screens/settings"
	"guizhi-mygo-shell/viva/tokens"
)

// 台账 C-008 / B-058 在设置页的接线：选图进行中「选择图片」禁用（N = 0.5），文字按整层淡出取色，
// 面板是设置卡片 SettingsCardBg；选图结束后恢复原色。期望值由「文字叠在面板上、不透明度 N」独立算出。
// 浅色下设置卡片与页面背景都是白的，接错面板令牌看不出来，所以再用深色外观跑一遍。
func TestBackgroundPickButtonInkFollowsTheCard(t *testing.T) {
	for _, mode := range []struct {
		name string
		dark bool
	}{{"浅色", false}, {"深色", true}} {
		t.Run(mode.name, func(t *testing.T) {
			release := make(chan struct{})
			started := make(chan struct{}, 1)
			store := sscreen.NewMemStore(nsettings.Default())
			r, a := openAppearanceWith(t, store, func(a *sscreen.Appearance) {
				if mode.dark {
					spec := kit.DefaultSpec()
					spec.Motion, spec.Mode = kit.MotionLevelOff, tokens.ModeDark
					a.Look = kit.NewLook(spec)
					a.Look.Scale, a.Look.Viewport = 1, 1280
				}
				a.PickBackground = func() (string, error) {
					started <- struct{}{}
					<-release // 一直开着选图窗口，直到测试放行
					return "", nil
				}
				a.Wake = func() {}
			})
			released := false
			t.Cleanup(func() {
				if !released {
					close(release)
				}
			})
			ink := func() (gfx.Color, bool) {
				for _, tx := range r.s.Snapshot().Texts {
					if tx.Value == locales.SettingsSelectBackgroundImage() {
						return tx.Style.Color, true
					}
				}
				return gfx.Color{}, false
			}
			tk := a.Look.T

			r.scrollToEnd()
			if got, ok := ink(); !ok || got != a.Look.Col(tk.PrimaryForeground) {
				t.Fatalf("选图之前的文字色 %+v（找到=%v），期望原色 %+v", got, ok, a.Look.Col(tk.PrimaryForeground))
			}
			r.clickNode(gfx.RoleButton, locales.SettingsSelectBackgroundImage())
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("选图函数没有被调用")
			}
			r.frames(3)
			if n, ok := r.node(gfx.RoleButton, locales.SettingsSelectBackgroundImage()); !ok || !n.Disabled {
				t.Fatalf("选图进行中「选择图片」应禁用：%+v ok=%v", n, ok)
			}
			got, ok := ink()
			if want := a.Look.Col(tk.PrimaryForeground.WithAlpha(0.5).Over(tk.SettingsCardBg)); !ok || got != want {
				t.Errorf("禁用的「选择图片」文字色 %+v（找到=%v），期望整层淡出的合成色 %+v", got, ok, want)
			}

			close(release)
			released = true
			deadline := time.Now().Add(2 * time.Second)
			for {
				r.frames(1)
				if n, ok := r.node(gfx.RoleButton, locales.SettingsSelectBackgroundImage()); ok && !n.Disabled {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("放行后按钮没有恢复可点")
				}
				time.Sleep(5 * time.Millisecond)
			}
			if got, ok := ink(); !ok || got != a.Look.Col(tk.PrimaryForeground) {
				t.Errorf("选图结束后的文字色 %+v（找到=%v），期望原色 %+v", got, ok, a.Look.Col(tk.PrimaryForeground))
			}
		})
	}
}
