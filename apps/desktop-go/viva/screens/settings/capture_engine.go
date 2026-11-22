package settings

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// 「采集与转写」页三个引擎（yt-dlp、ffmpeg、本地转写）共用的行外观（渲染层 capture/CaptureEngineRow.tsx）：
// 主行只留「名称 + 状态徽章 + 用途 + 主操作」，生效路径、自定义路径、重新检测、移除或卸载都收进折叠的高级面板。

// engineState 是引擎在界面上的归一化状态（渲染层 EngineState）。
type engineState uint8

const (
	engineProbing engineState = iota // 检测中：还没有任何可展示的状态
	engineReady
	engineMissing
	engineInvalid // 自定义路径无法运行
	engineError   // 探测失败
)

// enginePrimary 是主操作位：按钮，或没有可做动作时的静态状态（如「已是最新」）。
type enginePrimary struct {
	Label      string
	Icon       gfx.Icon
	Emphasized bool // 未安装与有更新时用实心主按钮，其余降级为描边按钮
	Busy       bool
	Status     bool // 静态状态而不是按钮
	OnClick    func()
}

// engineCustom 是高级面板里的自定义路径：只读显示，只能通过「选择文件」设置（路径会被外壳启动，不接受手输）。
type engineCustom struct {
	Value, Placeholder, Hint string
	OnPick, OnClear          func()
}

// engineAction 是高级面板里的维护动作；ConfirmMessage 非空时先弹破坏性确认框。
type engineAction struct {
	Label                     string
	ConfirmTitle, ConfirmText string
	Busy                      bool
	OnClick                   func()
}

// engineView 是一行此刻要画的内容；由各引擎的状态机每帧算出。
type engineView struct {
	ID, Name, Purpose string
	State             engineState
	Optional          bool
	Detail, BusyText  string
	ActivityLabel     string         // 非空时覆盖状态徽章（更新中、恢复中），避免同时声称引擎可用
	Activity          func(c ui.C)   // 主行下方始终可见的任务进度
	ActionsDisabled   bool           // 禁用维护动作（与动作自身是否在执行分开）
	Progress          int            // 安装进度百分比；< 0 表示没有进度条
	Primary           *enginePrimary // nil 表示没有主操作
	Refreshing        bool
	OnRefresh         func()
	ActivePath        string
	Custom            *engineCustom
	Reinstall, Remove *engineAction
}

// engineRowState 是一行的界面状态（折叠与确认框）；零值可用。
type engineRowState struct {
	expanded   bool
	lastState  engineState
	seen       bool
	confirming bool
	confirm    kit.ConfirmState
}

func (s *engineRowState) sync(state engineState) {
	// 自定义路径跑不起来时摊开面板，否则用户看不到出问题的那个输入框；
	// 只在状态切换到「路径无效」的那一刻展开，之后用户手动收起不会被再次弹开。
	if state == engineInvalid && (!s.seen || s.lastState != engineInvalid) {
		s.expanded = true
	}
	s.lastState, s.seen = state, true
}

// ── 外观 ──

func engineTone(l *kit.Look, state engineState, optional bool) (border, bg, fg tokens.Color) {
	t := l.T
	pick := func(light, dark string) tokens.Color {
		if l.Dark {
			return tokens.Tailwind(dark)
		}
		return tokens.Tailwind(light)
	}
	switch {
	case state == engineReady:
		return tokens.Tailwind("emerald-600").WithAlpha(0.4), tokens.Tailwind("emerald-500").WithAlpha(0.1), pick("emerald-800", "emerald-200")
	case state == engineInvalid || state == engineError:
		return t.Destructive.WithAlpha(0.3), t.Destructive.WithAlpha(0.1), t.Destructive
	case state == engineMissing && !optional:
		return tokens.Tailwind("amber-600").WithAlpha(0.4), tokens.Tailwind("amber-500").WithAlpha(0.1), pick("amber-800", "amber-200")
	}
	return t.Border, t.Muted.WithAlpha(0.6), t.MutedForeground
}

func engineStateLabel(state engineState, optional bool) string {
	switch state {
	case engineProbing:
		return locales.SettingsCaptureStateProbing()
	case engineReady:
		return locales.SettingsCaptureStateReady()
	case engineInvalid:
		return locales.SettingsCaptureStateInvalid()
	case engineError:
		return locales.SettingsCaptureStateError()
	}
	if optional {
		return locales.SettingsCaptureStateOptional()
	}
	return locales.SettingsCaptureStateMissing()
}

// badge 画一枚带边框的圆角全满徽章：图标加文字，px-2 py-0.5 text-xs font-medium。
func engineBadge(l *kit.Look, c ui.C, label string, icon gfx.Icon, spin bool, border, bg, fg tokens.Color) {
	ui.Row(c).Shrink(0).Items(ui.Center).Gap(l.Space(1)).PadSides(l.Space(0.5)+1, l.Space(2)+1, l.Space(0.5)+1, l.Space(2)+1).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: bg, Border: border, BorderW: 1, Radius: d.Rect.Height / 2})
		}).
		Children(func() {
			if spin {
				l.Spinner(c.Key("spin"), l.Rem(0.75), fg)
			} else {
				l.Icon(c.Key("icon"), icon, l.Rem(0.75), fg)
			}
			l.Text(c.Key("label"), label, kit.TextXS, kit.WeightMedium, fg)
		})
}

func stateIcon(state engineState, optional bool) gfx.Icon {
	switch {
	case state == engineReady:
		return icons.CircleCheck
	case state == engineMissing && optional:
		return icons.CircleDashed
	}
	return icons.TriangleAlert
}

// progressBar 是 h-1.5 的圆角进度条：轨道 + 填充（按百分比取宽）。
func progressBar(l *kit.Look, c ui.C, percent int, track, fill tokens.Color) {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	ui.Row(c).FillWidth().Height(l.Rem(0.375)).Draw(func(d *ui.DrawContext) {
		l.PaintSurface(d, kit.Surface{Bg: track, Radius: d.Rect.Height / 2})
		if w := d.Rect.Width * float32(percent) / 100; w > 0 {
			l.PaintSurfaceAt(d, gfx.Rect{X: d.Rect.X, Y: d.Rect.Y, Width: w, Height: d.Rect.Height}, kit.Surface{Bg: fill, Radius: d.Rect.Height / 2})
		}
	})
}

// ── 行 ──

// engineRow 声明一行引擎；st 由调用方持有（折叠与确认框状态）。
func engineRow(l *kit.Look, c ui.C, st *engineRowState, v engineView) {
	st.sync(v.State)
	l.SettingBlock(c, false, func() {
		engineMain(l, c.Key("main"), st, v)
		if v.Activity != nil {
			v.Activity(c.Key("activity"))
		}
		if v.Progress >= 0 {
			ui.Column(c.Key("progress")).FillWidth().PadSides(0, l.Space(4), l.Space(3), l.Space(4)).Children(func() {
				progressBar(l, c, v.Progress, l.T.Muted, l.T.Primary)
			})
		}
		if st.expanded {
			enginePanel(l, c.Key("panel"), st, v)
		}
		if v.Remove != nil {
			engineConfirm(l, c.Key("confirm"), st, v.Remove)
		}
	})
}

func engineMain(l *kit.Look, c ui.C, st *engineRowState, v engineView) {
	t := l.T
	description := v.BusyText
	if description == "" {
		description = v.Purpose
		if v.Detail != "" {
			description += " · " + v.Detail
		}
	}
	ui.Row(c).FillWidth().Items(ui.Center).Justify(ui.Between).Gap(l.Space(4)).
		PadSides(l.Space(3), l.Space(4), l.Space(3), l.Space(4)).Children(func() {
		ui.Column(c.Key("text")).Shrink(1).MinWidth(0).Children(func() {
			ui.Row(c.Key("title")).Items(ui.Center).Gap(l.Space(2)).Children(func() {
				l.Text(c.Key("name"), v.Name, kit.TextSM, kit.WeightMedium, t.Foreground)
				if v.ActivityLabel != "" {
					engineBadge(l, c.Key("activity"), v.ActivityLabel, icons.CircleDashed, false,
						t.Primary.WithAlpha(0.25), t.Primary.WithAlpha(0.1), t.Primary)
					return
				}
				border, bg, fg := engineTone(l, v.State, v.Optional)
				engineBadge(l, c.Key("badge"), engineStateLabel(v.State, v.Optional), stateIcon(v.State, v.Optional),
					v.State == engineProbing, border, bg, fg)
			})
			l.Text(c.Key("desc"), description, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped()).MarginTop(l.Space(0.5))
		})
		ui.Row(c.Key("actions")).Shrink(0).Items(ui.Center).Gap(l.Space(2)).Children(func() {
			engineActions(l, c, st, v)
		})
	})
}

func engineActions(l *kit.Look, c ui.C, st *engineRowState, v engineView) {
	t := l.T
	big := shape(l, 2.25, 3, 1.5, kit.TextSM, kit.WeightMedium) // h-9 px-3 gap-1.5 text-sm font-medium
	if p := v.Primary; p != nil {
		if p.Status {
			fg := tokens.Tailwind("emerald-600")
			if l.Dark {
				fg = tokens.Tailwind("emerald-400")
			}
			ui.Row(c.Key("status")).Height(l.Rem(2.25)).Items(ui.Center).Gap(l.Space(1.5)).PadSides(0, l.Space(1), 0, l.Space(1)).Children(func() {
				l.Icon(c, icons.CircleCheck, l.Rem(1), fg)
				l.Text(c, p.Label, kit.TextSM, kit.WeightNormal, fg)
			})
		} else {
			style := outlineBtn(l, big, t.Muted.WithAlpha(0.6))
			if p.Emphasized {
				style = primaryBtn(l, big)
			}
			style.DisabledOpacity = 0.6 // disabled:opacity-60
			ic, size := iconOf(l, p.Icon, false)
			if btn(l, c.Key("primary"), kit.ButtonSpec{Label: p.Label, Icon: ic, IconSize: size}, style, p.Busy) {
				p.OnClick()
			}
		}
	}
	chevron := icons.ChevronDown
	if st.expanded {
		chevron = icons.ChevronUp // rotate-180
	}
	adv := locales.SettingsCaptureAdvanced()
	style := iconBtn(l, l.Rem(2.25), t.Muted.WithAlpha(0.6), t.Foreground)
	if l.Button(c.Key("advanced"), kit.ButtonSpec{Name: adv, Hint: adv, Icon: chevron, IconSize: l.Rem(1), Center: true, Disclosure: true, On: st.expanded}, style).Clicked() {
		st.expanded = !st.expanded
	}
}

// enginePanel 是折叠的高级面板：space-y-3 border-t border-border/70 bg-muted/20 px-4 py-3。
func enginePanel(l *kit.Look, c ui.C, st *engineRowState, v engineView) {
	t := l.T
	ui.Column(c).FillWidth().Gap(l.Space(3)).PadSides(l.Space(3)+1, l.Space(4), l.Space(3), l.Space(4)).
		Draw(func(d *ui.DrawContext) {
			d.Canvas.Fill(d.Rect, d.Fade(l.Col(t.Muted.WithAlpha(0.2))))
		}).
		DrawOver(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeTop, 1, t.Border.WithAlpha(0.7)) }).
		Children(func() {
			if v.ActivePath != "" {
				ui.Row(c.Key("active")).FillWidth().Items(ui.Center).Gap(l.Space(3)).Children(func() {
					l.Text(c, locales.SettingsCaptureActivePath(), kit.TextXS, kit.WeightNormal, t.MutedForeground).Width(l.Rem(5)).Shrink(0)
					ui.Row(c.Key("code")).Grow(1).MinWidth(0).PadSides(l.Space(2), l.Space(3), l.Space(2), l.Space(3)).
						Draw(func(d *ui.DrawContext) {
							l.PaintSurface(d, kit.Surface{Bg: t.Muted.WithAlpha(0.6), Radius: l.Rem(0.75)})
						}).Children(func() {
						l.Text(c, v.ActivePath, kit.TextXS, kit.WeightNormal, t.Foreground, kit.Ellipsis()).MinWidth(0)
					})
				})
			}
			if v.Custom != nil {
				engineCustomPath(l, c.Key("custom"), v.Custom)
			}
			engineMaintenance(l, c.Key("maintenance"), st, v)
		})
}

func engineCustomPath(l *kit.Look, c ui.C, cp *engineCustom) {
	t := l.T
	ui.Row(c).FillWidth().Gap(l.Space(3)).Children(func() {
		l.Text(c.Key("label"), locales.SettingsCaptureCustomPath(), kit.TextXS, kit.WeightNormal, t.MutedForeground).
			Width(l.Rem(5)).Shrink(0).MarginTop(l.Space(3))
		ui.Column(c.Key("body")).Grow(1).MinWidth(0).Children(func() {
			ui.Row(c.Key("row")).FillWidth().Items(ui.Center).Gap(l.Space(2)).Children(func() {
				readOnlyPath(l, c.Key("value"), locales.SettingsCaptureCustomPath(), cp.Value, cp.Placeholder)
				square := iconBtn(l, l.Rem(2.5), t.Muted.WithAlpha(0.6), t.Foreground)
				square.Border, square.BorderW = t.Border, 1
				browse := locales.SettingsCaptureBrowse()
				if l.Button(c.Key("browse"), kit.ButtonSpec{Name: browse, Hint: browse, Icon: icons.FolderOpen, IconSize: l.Rem(1), Center: true}, square).Clicked() {
					cp.OnPick()
				}
				if cp.Value != "" {
					clear := locales.SettingsCaptureClearPath()
					if l.Button(c.Key("clear"), kit.ButtonSpec{Name: clear, Hint: clear, Icon: icons.X, IconSize: l.Rem(1), Center: true}, square).Clicked() {
						cp.OnClear()
					}
				}
			})
			l.Text(c.Key("hint"), cp.Hint, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped()).MarginTop(l.Space(1.5))
		})
	})
}

// readOnlyPath 是只读的路径框（渲染层 <Input readOnly>）：外观同表单文本框，但不接收输入——
// 路径会被外壳启动，只接受文件选择器返回的值。可访问名取 label，当前值放在可访问描述里。
func readOnlyPath(l *kit.Look, c ui.C, label, value, placeholder string) {
	t := l.T
	ink, text := t.Foreground, value
	if value == "" {
		ink, text = t.MutedForeground, placeholder
	}
	ui.Row(c).Grow(1).MinWidth(0).Height(l.Rem(2.5)).Items(ui.Center).PadSides(0, l.Space(4)+1, 0, l.Space(4)+1).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Muted.WithAlpha(0.5), Border: t.Border.WithAlpha(0.7), BorderW: 1, Radius: l.Rem(0.75)})
			d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleGroup, label, gfx.HitOptions{Description: value})
		}).Children(func() {
		l.Text(c, text, kit.TextSM, kit.WeightNormal, ink, kit.Ellipsis()).MinWidth(0)
	})
}

func engineMaintenance(l *kit.Look, c ui.C, st *engineRowState, v engineView) {
	t := l.T
	small := shape(l, 2, 2.5, 1.5, kit.TextXS, kit.WeightMedium) // h-8 px-2.5 gap-1.5 text-xs font-medium
	muted := func() kit.ButtonStyle {
		s := outlineBtn(l, small, t.Muted.WithAlpha(0.6))
		s.Fg, s.FgHover = t.MutedForeground, t.Foreground
		s.DisabledOpacity = 0.6
		return s
	}
	ui.Row(c).FillWidth().Items(ui.Center).Justify(ui.Between).Gap(l.Space(3)).PadSides(l.Space(0.5), 0, 0, 0).Children(func() {
		ic, size := iconOf(l, icons.RefreshCw, true)
		if btn(l, c.Key("recheck"), kit.ButtonSpec{Label: locales.SettingsCaptureRecheck(), Icon: ic, IconSize: size, Disabled: v.Refreshing || v.ActionsDisabled},
			muted(), v.Refreshing) {
			v.OnRefresh()
		}
		ui.Row(c.Key("extra")).Items(ui.Center).Gap(l.Space(2)).Children(func() {
			if a := v.Reinstall; a != nil {
				ic, size := iconOf(l, icons.RefreshCw, true)
				if btn(l, c.Key("reinstall"), kit.ButtonSpec{Label: a.Label, Icon: ic, IconSize: size, Disabled: a.Busy || v.ActionsDisabled}, muted(), a.Busy) {
					a.OnClick()
				}
			}
			if a := v.Remove; a != nil {
				s := muted()
				s.BorderHover, s.BgHover, s.FgHover = t.Destructive.WithAlpha(0.4), t.Destructive.WithAlpha(0.1), t.Destructive
				ic, size := iconOf(l, icons.Trash2, true)
				if btn(l, c.Key("remove"), kit.ButtonSpec{Label: a.Label, Icon: ic, IconSize: size, Disabled: a.Busy || v.ActionsDisabled}, s, a.Busy) {
					st.confirming = true
				}
			}
		})
	})
}

// engineConfirm 声明移除或卸载的破坏性确认框（每帧都要经过，关闭那一帧由 ConfirmDialog 复位）。
func engineConfirm(l *kit.Look, c ui.C, st *engineRowState, a *engineAction) {
	switch l.ConfirmDialog(c, &st.confirm, kit.ConfirmSpec{
		Open: st.confirming, Title: a.ConfirmTitle, Message: a.ConfirmText,
		ConfirmText: a.Label, CancelText: locales.CommonCancel(), Destructive: true,
	}) {
	case kit.ConfirmAccepted:
		st.confirming = false
		a.OnClick()
	case kit.ConfirmDismissed:
		st.confirming = false
	}
}
