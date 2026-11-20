package library

import (
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// 本文件守住台账 C-008 / B-058 在库界面的接线：有底色的禁用按钮，文字按渲染层的「整层淡出」取色，
// 所以按钮要告诉 kit.Button 它背后的面板是什么（ButtonStyle.Surface）。期望值由「文字叠在面板上、不透明度 N」独立算出。
// 浅色下弹窗、浮层与页面背景都是白的，接错面板令牌看不出来；每个用例因此再用深色外观跑一遍（深色下这几张面板各不相同）。

// inkColor 是画面上第一处恰好等于 text 的文字的颜色；没有这行文字时 ok 为假。
func (r *rig) inkColor(text string) (gfx.Color, bool) {
	for _, t := range r.session.Snapshot().Texts {
		if t.Value == text {
			return t.Style.Color, true
		}
	}
	return gfx.Color{}, false
}

// inkModes 是接线用例要跑的两种外观：nil 是 newRig 默认的浅色，darkRig 换成深色。
var inkModes = []struct {
	name   string
	mutate []func(*rig)
}{{"浅色", nil}, {"深色", []func(*rig){darkRig}}}

// darkRig 把外观换成深色（首帧之前调用）。
func darkRig(r *rig) {
	spec := kit.DefaultSpec()
	spec.Motion, spec.Mode = kit.MotionLevelOff, tokens.ModeDark
	r.look = kit.NewLook(spec)
	r.look.Scale, r.look.Viewport = 1, 1280
	r.screen.Look = r.look
}

// 新建合集弹窗的「确认」：名称为空时禁用（N = 0.5），面板是弹窗面板 UiPanelStrongBg；填了名称就是原色。
func TestNewCollectionConfirmInkFollowsTheDialogPanel(t *testing.T) {
	for _, mode := range inkModes {
		t.Run(mode.name, func(t *testing.T) {
			r := newRig(t, mode.mutate...)
			tk := r.look.T
			r.click(gfx.RoleButton, locales.LibraryNewCollection())
			if !r.mustNode(gfx.RoleButton, locales.CommonConfirm()).Disabled {
				t.Fatal("名称为空时「确认」应禁用")
			}
			got, ok := r.inkColor(locales.CommonConfirm())
			if want := r.look.Col(tk.PrimaryForeground.WithAlpha(0.5).Over(tk.UiPanelStrongBg)); !ok || got != want {
				t.Errorf("禁用的「确认」文字色 %+v（找到=%v），期望整层淡出的合成色 %+v", got, ok, want)
			}
			r.typeText("读书")
			if r.mustNode(gfx.RoleButton, locales.CommonConfirm()).Disabled {
				t.Fatal("有名称时「确认」应可用")
			}
			got, ok = r.inkColor(locales.CommonConfirm())
			if want := r.look.Col(tk.PrimaryForeground); !ok || got != want {
				t.Errorf("可用的「确认」文字色 %+v（找到=%v），期望原色 %+v", got, ok, want)
			}
		})
	}
}

// 批量打标签浮层的「添加」：草稿为空时禁用（N = 0.4），面板是浮层 Popover；有草稿就是原色。
func TestBulkTagAddInkFollowsThePopover(t *testing.T) {
	t.Parallel()
	for _, mode := range inkModes {
		t.Run(mode.name, func(t *testing.T) {
			bothViews(t, func(t *testing.T, r *rig) {
				tk := r.look.T
				r.selectIDs(r.ids(2, 4))
				r.click(gfx.RoleButton, locales.LibraryBulkAddTag())
				if !r.mustNode(gfx.RoleButton, locales.CommonAdd()).Disabled {
					t.Fatal("草稿为空时「添加」应禁用")
				}
				got, ok := r.inkColor(locales.CommonAdd())
				if want := r.look.Col(tk.PrimaryForeground.WithAlpha(0.4).Over(tk.Popover)); !ok || got != want {
					t.Errorf("禁用的「添加」文字色 %+v（找到=%v），期望整层淡出的合成色 %+v", got, ok, want)
				}
				r.typeText("新标签")
				got, ok = r.inkColor(locales.CommonAdd())
				if want := r.look.Col(tk.PrimaryForeground); !ok || got != want {
					t.Errorf("可用的「添加」文字色 %+v（找到=%v），期望原色 %+v", got, ok, want)
				}
			}, mode.mutate...)
		})
	}
}
