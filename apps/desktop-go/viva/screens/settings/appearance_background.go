package settings

import (
	"errors"
	"math"
	"strconv"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// errPickerUnavailable 是宿主没有接上选图时的失败原因（进「查看详情」与日志，不进界面文案）。
var errPickerUnavailable = errors.New("background image picker is not available in this build")

// mdBreakpoint 是 Tailwind md 断点（768px 视口宽度）：不窄于它时两个滑块并排。
const mdBreakpoint = 768

// backgroundImage 是「背景图」分区（桌面端才有）：标题与说明、选图与开关按钮、启用状态提示，
// 下面一块轻底卡片：16:9 的预览舞台加可见度、虚化两个滑块。
func (a *Appearance) backgroundImage(c ui.C) {
	l := a.Look
	cur := a.Store.Values().UI
	hasImage := cur.BackgroundImageFileName != ""
	enabled := hasImage && cur.BackgroundImageEnabled
	l.SettingSectionBody(c, locales.SettingsBackgroundImage(), func() {
		ui.Column(c).FillWidth().Pad(l.Space(4)).Gap(l.Space(4)).Children(func() {
			a.backgroundHeader(c, hasImage, enabled)
			if hasImage {
				hint := locales.SettingsBackgroundImageDisabledHint()
				if enabled {
					hint = locales.SettingsBackgroundImageEnabledHint()
				}
				l.Text(c, hint, kit.TextXS, kit.WeightNormal, l.T.MutedForeground, kit.Wrapped())
			}
			a.backgroundTuning(c, cur)
		})
	})
}

// backgroundHeader 是顶部一行：左边标题与说明，右边「选择/更换图片」与「开启/关闭」两个按钮。
func (a *Appearance) backgroundHeader(c ui.C, hasImage, enabled bool) {
	l := a.Look
	t := l.T
	ui.Row(c.Key("header")).FillWidth().Items(ui.Start).Justify(ui.Between).Gap(l.Space(3)).Children(func() {
		ui.Column(c).Shrink(1).MinWidth(0).Children(func() {
			ui.Row(c).Items(ui.Center).Gap(l.Space(2)).Children(func() {
				l.Icon(c, icons.Image, l.Rem(1), t.MutedForeground)
				l.Text(c, locales.SettingsBackgroundImageTitle(), kit.TextSM, kit.WeightMedium, t.Foreground)
			})
			l.TextPx(c, locales.SettingsBackgroundImageDesc(), l.Rem(0.75), l.Rem(1.5), kit.WeightNormal, t.MutedForeground, kit.Wrapped()).
				MarginTop(l.Space(1))
		})
		ui.Row(c).Shrink(0).Items(ui.Center).Gap(l.Space(2)).Children(func() {
			pick := locales.SettingsSelectBackgroundImage()
			if hasImage {
				pick = locales.SettingsChangeBackgroundImage()
			}
			primary := l.Button(c.Key("pick"), kit.ButtonSpec{Label: pick, Disabled: a.picking}, kit.ButtonStyle{
				Height: l.Rem(2.25), PadX: l.Space(4), Radius: l.Rem(0.75), Text: kit.TextSM, Weight: kit.WeightMedium, // rounded-lg
				Fg: t.PrimaryForeground, Bg: t.Primary, BgHover: t.Primary.WithAlpha(0.9),
				Transition: kit.DurationDefault, DisabledOpacity: 0.5,
			})
			if primary.Clicked() {
				a.startPick()
			}
			toggle := locales.SettingsEnableBackgroundImage()
			if enabled {
				toggle = locales.SettingsDisableBackgroundImage()
			}
			// app-wallpaper-surface 的底色、边框色与阴影带 !important，盖过 border-border 与 hover:bg-accent/60：悬停不变色。
			secondary := l.Button(c.Key("toggle"), kit.ButtonSpec{Label: toggle, Disabled: !hasImage}, kit.ButtonStyle{
				Height: l.Rem(2.25), PadX: l.Space(3), Radius: l.Rem(0.75), Gap: l.Space(2), Text: kit.TextSM, Weight: kit.WeightNormal, // rounded-lg
				Fg: t.Foreground, Bg: t.UiSurfaceBg, Border: t.UiSurfaceBorder, BorderW: 1, Shadow: t.UiSurfaceShadow,
				Transition: kit.DurationDefault, DisabledOpacity: 0.4,
			})
			if secondary.Clicked() {
				a.save(func(v *settings.Values) { v.UI.BackgroundImageEnabled = !v.UI.BackgroundImageEnabled })
			}
		})
	})
}

// backgroundTuning 是轻底卡片：预览舞台与两个滑块（md 以上并排，否则上下叠放）。
func (a *Appearance) backgroundTuning(c ui.C, cur settings.UI) {
	l := a.Look
	t := l.T
	card := kit.Surface{Bg: t.SettingsSubtleBg, Border: t.SettingsSubtleBorder, BorderW: 1, Radius: l.Rem(1)}
	ui.Column(c.Key("tuning")).FillWidth().Pad(l.Space(3) + 1).Gap(l.Space(3)).
		Draw(func(d *ui.DrawContext) { l.PaintSurface(d, card) }).
		Children(func() {
			a.previewStage(c, cur)
			sliders := func() {
				a.opacitySlider(c, cur)
				a.blurSlider(c, cur)
			}
			a.sideBySide = l.ViewportWidth(c) >= mdBreakpoint
			if a.sideBySide {
				ui.Row(c.Key("sliders")).FillWidth().Gap(l.Space(4)).Children(sliders)
			} else {
				ui.Column(c.Key("sliders")).FillWidth().Gap(l.Space(4)).Children(sliders)
			}
		})
}

// previewStage 是 16:9 的预览舞台（app-settings-input，rounded-xl）。没有背景图时是居中的空态：
// 32px 图标（opacity-50）加 text-sm 说明，gap-2。
func (a *Appearance) previewStage(c ui.C, cur settings.UI) {
	l := a.Look
	t := l.T
	surface := kit.Surface{Bg: t.SettingsInputBg, Border: t.SettingsInputBorder, BorderW: 1, Radius: l.Rem(0.75)}
	stage := l.AspectBox(c.Key("stage"), 16.0/9, func() {
		if cur.BackgroundImageFileName != "" {
			return // 有图时的预览（壁纸与示意界面）尚未迁移，见台账 B-020
		}
		ui.Column(c).Items(ui.Center).Justify(ui.Center).Gap(l.Space(2)).Children(func() {
			l.Icon(c, icons.Image, l.Rem(2), kit.Fade(t.MutedForeground, 0.5))
			l.Text(c, locales.SettingsBackgroundImageEmpty(), kit.TextSM, kit.WeightNormal, t.MutedForeground)
		})
	})
	stage.Draw(func(d *ui.DrawContext) { l.PaintSurface(d, surface) })
}

// sliderBlock 是一个滑块块（space-y-2）：上面是「图标 + 标签」与右侧数值，下面是滑块。
// 并排时（grid md:grid-cols-2）两块等宽，叠放时各占满一行。
func (a *Appearance) sliderBlock(c ui.C, key string, icon gfx.Icon, label string, value func(), slider func()) {
	l := a.Look
	t := l.T
	// <input type="range"> 是 inline-block：在块容器里按基线排，滑块下面多出支撑文字的下降量（见 kit/inlinegap.go）。
	block := ui.Column(c.Key(key)).Gap(l.Space(2)).PadSides(0, 0, l.StrutDescent(), 0)
	if a.sideBySide {
		block = block.Grow(1).Basis(0).MinWidth(0)
	} else {
		block = block.FillWidth()
	}
	block.Children(func() {
		ui.Row(c).FillWidth().Items(ui.Center).Justify(ui.Between).Gap(l.Space(3)).Children(func() {
			ui.Row(c).Items(ui.Center).Gap(l.Space(1.5)).Children(func() {
				l.Icon(c, icon, l.Rem(0.875), t.MutedForeground)
				l.Text(c, label, kit.TextXS, kit.WeightNormal, t.MutedForeground)
			})
			value()
		})
		slider()
	})
}

// syncSlider 把设置里的值灌进滑块，只在设置变了（第一帧、别处改了、写入失败后退回）时才灌：
// 键盘改值发生在两次声明之间，滑块要到下一次声明才报告「变了」，每帧无条件覆盖会在报告之前把这次修改冲掉。
// seen 是上次灌进去的设置值。
func syncSlider(state *ui.SliderState, seen *float64, value float64) {
	if *seen != value {
		state.Value, *seen = float32(value), value
	}
}

// opacitySlider 是「背景可见度」：0–100%，步长 1；写入时除以 100 并钳到 0–1。
func (a *Appearance) opacitySlider(c ui.C, cur settings.UI) {
	l := a.Look
	t := l.T
	percent := math.Round(cur.BackgroundImageOpacity * 100)
	a.sliderBlock(c, "opacity", icons.Image, locales.SettingsBackgroundImageOpacity(),
		func() {
			l.TextRuns(c, kit.TextXS, kit.WeightNormal, t.MutedForeground, strconv.FormatFloat(percent, 'f', -1, 64), "%")
		},
		func() {
			syncSlider(&a.opacity, &a.opacitySeen, percent)
			if l.RangeInput(c, &a.opacity, kit.RangeSpec{Name: locales.SettingsBackgroundImageOpacity(), Min: 0, Max: 100, Step: 1}) {
				next := settings.ClampOpacity(float64(a.opacity.Value) / 100)
				if next == cur.BackgroundImageOpacity || !a.save(func(v *settings.Values) { v.UI.BackgroundImageOpacity = next }) {
					a.opacity.Value = float32(percent) // 没有写入（值没变，或写入失败）：滑块退回设置里的值
				}
			}
		})
}

// blurSlider 是「虚化强度」：0–50px，步长 0.5。
func (a *Appearance) blurSlider(c ui.C, cur settings.UI) {
	l := a.Look
	t := l.T
	a.sliderBlock(c, "blur", icons.SlidersHorizontal, locales.SettingsBackgroundImageBlur(),
		func() {
			l.TextRuns(c, kit.TextXS, kit.WeightNormal, t.MutedForeground, strconv.FormatFloat(cur.BackgroundImageBlur, 'f', -1, 64), "px")
		},
		func() {
			syncSlider(&a.blur, &a.blurSeen, cur.BackgroundImageBlur)
			if l.RangeInput(c, &a.blur, kit.RangeSpec{Name: locales.SettingsBackgroundImageBlur(), Min: 0, Max: 50, Step: 0.5}) {
				next := settings.ClampBlur(float64(a.blur.Value))
				if next == cur.BackgroundImageBlur || !a.save(func(v *settings.Values) { v.UI.BackgroundImageBlur = next }) {
					a.blur.Value = float32(cur.BackgroundImageBlur)
				}
			}
		})
}

// startPick 开始选图（渲染层 handleSelectBackgroundImage）：防重入；选图与存图在后台线程里做，
// 结果由 finishPick 在下一帧取走。宿主没接上选图时如实报错，而不是点了没反应。
func (a *Appearance) startPick() {
	if a.picking {
		return
	}
	if a.PickBackground == nil {
		a.failPick(errPickerUnavailable)
		return
	}
	a.picking = true
	go func() {
		name, err := a.PickBackground()
		a.pickMu.Lock()
		a.pickResult = &pickOutcome{fileName: name, err: err}
		a.pickMu.Unlock()
		if a.Wake != nil {
			a.Wake()
		}
	}()
}

// finishPick 取走后台选图的结果并应用：成功则记下文件名并自动启用（applyBackgroundImageSelection），
// 取消什么也不做，失败弹出错误提示（原文放在「查看详情」里）并记日志。
func (a *Appearance) finishPick() {
	a.pickMu.Lock()
	res := a.pickResult
	a.pickResult = nil
	a.pickMu.Unlock()
	if res == nil {
		return
	}
	a.picking = false
	switch {
	case res.err != nil:
		a.failPick(res.err)
	case res.fileName == "":
		// 用户取消，或存图没有返回文件名：渲染层把后者也当作失败提示；取消则 selectImage 就返回了空列表。
	default:
		name := settings.NormalizeBackgroundFileName(res.fileName)
		if name == "" {
			return
		}
		a.save(func(v *settings.Values) {
			v.UI.BackgroundImageEnabled = true
			v.UI.BackgroundImageFileName = name
		})
	}
}

// failPick 报告选图失败：渲染层弹出固定的错误提示（settings.backgroundImageSaveFailed）；
// 这里同样，并把原因放进「查看详情」、写进日志，用户可以复制并重试。
func (a *Appearance) failPick(err error) {
	a.Guard.Background("selectBackgroundImage", err)
	if a.Guard.Notify != nil {
		a.Guard.Notify.Error(locales.SettingsBackgroundImageSaveFailed(), err.Error())
	}
}
