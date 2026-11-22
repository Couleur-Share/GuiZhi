package settings

import (
	"context"
	"errors"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// red500 是 Tailwind 的 text-red-500（冲突提示）；hitButton 是图标按钮的命中语义。
var (
	red500    = tokens.RGBA(239, 68, 68, 1)
	hitButton = gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand}
)

// errShortcutsUnavailable 表示外壳没有接快捷键服务：保存不了，必须如实说，不能弹一句「已更新」。
var errShortcutsUnavailable = errors.New("shortcut service is not available")

// textXSNormal 是 text-xs leading-normal：12px 字、行高 1.5 倍（18px）。
var textXSNormal = kit.TextStyle{Size: 0.75, Line: 0.75 * 1.5}

// shortcutItem 是一项可设置的快捷键：动作键（与主进程一致）、标签与说明。
type shortcutItem struct {
	action      string
	label, desc func() string
}

// shortcutItems 是设置页列出的全部快捷键，顺序同渲染层 ShortcutsSettings。
var shortcutItems = []shortcutItem{
	{services.ShortcutShowApp, locales.SettingsShortcutShowApp, locales.SettingsShortcutShowAppDesc},
	{services.ShortcutNewItem, locales.SettingsShortcutNewItem, locales.SettingsShortcutNewItemDesc},
	{services.ShortcutSearch, locales.SettingsShortcutSearch, locales.SettingsShortcutSearchDesc},
	{services.ShortcutSettings, locales.SettingsShortcutSettings, locales.SettingsShortcutSettingsDesc},
}

// Shortcuts 是「快捷键」页（渲染层 ShortcutsSettings）。
type Shortcuts struct {
	Look *kit.Look
	// Guard 接住保存与加载的失败：错误提示带可复制的原因并记日志。
	Guard kit.Guard
	// Service 读写快捷键；nil 表示外壳没接：界面显示默认值，保存时如实报错。
	Service services.Shortcuts
	// Wake 请求窗口重绘：后台加载完成后调用。可为 nil。
	Wake func()

	accelerators map[string]string // 动作 → 快捷键；空串表示未设置
	modes        map[string]string // 动作 → global / local
	conflicts    map[string]string // 动作 → 与它冲突的动作（只在提交被拒时记，改成功或清除后移除）
	recorders    [4]recorder
	modeSels     [4]kit.SelectState

	started bool
	cancel  context.CancelFunc
	mb      mailbox
}

// Close 在分区被切走时调用：丢弃还没送达的加载结果。渲染层的分区卸载后重新挂载会重新读取，所以状态也一并重置。
func (s *Shortcuts) Close() {
	s.mb.close()
	if s.cancel != nil {
		s.cancel()
	}
}

// init 填入默认值（渲染层的 useState 初值：与主进程 DEFAULT_SHORTCUTS 一致，避免读到真实配置前闪错值）。
func (s *Shortcuts) init() {
	if s.accelerators != nil {
		return
	}
	s.accelerators = services.DefaultShortcuts()
	s.modes = services.DefaultShortcutModes()
	s.conflicts = map[string]string{}
}

// load 在首次声明时发起读取（渲染层 useEffect 里的 loadShortcuts）。失败弹出带原因的错误提示，界面保持默认值。
func (s *Shortcuts) load() {
	if s.started || s.Service == nil {
		return
	}
	s.started = true
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	svc, wake := s.Service, s.Wake
	go func() {
		cfg, err := svc.Get(ctx)
		if ctx.Err() != nil {
			return
		}
		s.mb.post(wake, func() {
			if err != nil {
				s.Guard.Fail(locales.SettingsShortcutLoadFailed(), locales.SettingsShortcutLoadFailed(), err)
				return
			}
			if cfg.Accelerators != nil {
				s.accelerators = cloneStrings(cfg.Accelerators)
			}
			for _, it := range shortcutItems {
				if mode := cfg.Modes[it.action]; mode != "" {
					s.modes[it.action] = mode
				}
			}
		})
	}()
}

// Page 声明整页：快捷键分区加使用提示分区。
func (s *Shortcuts) Page(c ui.C) {
	l := s.Look
	s.init()
	s.mb.drain()
	s.load()
	l.SettingsContent(c, locales.SettingsShortcuts(), func() {
		l.SettingSections(c, func() {
			l.SettingSectionBody(c, locales.SettingsShortcutOptions(), func() {
				for i, it := range shortcutItems {
					s.item(c.Key(it.action), i, it)
				}
			})
			l.SettingSectionBody(c.Key("tips"), locales.SettingsShortcutTips(), func() { s.tips(c) })
		})
	})
}

// tips 是使用提示（p-4 text-sm text-muted-foreground space-y-2）。
func (s *Shortcuts) tips(c ui.C) {
	l := s.Look
	ui.Column(c).FillWidth().Pad(l.Space(4)).Gap(l.Space(2)).Children(func() {
		for i, tip := range []string{
			locales.SettingsShortcutTip1(), locales.SettingsShortcutTip2(), locales.SettingsShortcutTip3(), locales.SettingsShortcutModeDesc(),
		} {
			// 渲染层是 <p>• {tip}</p>：「• 」与正文是两个相邻文本节点，拆成两段画对照才逐段对得上。
			l.TextRuns(c.Key(string(rune('a'+i))), kit.TextSM, kit.WeightNormal, l.T.MutedForeground, "• ", tip)
		}
	})
}

// item 声明一项快捷键：左侧标签、说明与冲突提示，右侧模式下拉、录制框与清除按钮。
func (s *Shortcuts) item(c ui.C, idx int, it shortcutItem) {
	l := s.Look
	t := l.T
	label := it.label()
	l.ShortcutRow(c, idx == len(shortcutItems)-1, func() {
		l.Text(c, label, kit.TextSM, kit.WeightMedium, t.Foreground, kit.Wrapped())
		l.Text(c.Key("desc"), it.desc(), textXSNormal, kit.WeightNormal, t.MutedForeground, kit.Wrapped()).MarginTop(l.Space(1.5))
		if other := s.conflicts[it.action]; other != "" {
			ui.Row(c.Key("conflict")).Items(ui.Center).Gap(l.Space(1.5)).MarginTop(l.Space(2)).Children(func() {
				l.Icon(c, icons.TriangleAlert, l.Space(3.5), red500)
				l.Text(c.Key("text"), locales.SettingsConflictWith(s.labelOf(other)), kit.TextXS, kit.WeightMedium, red500, kit.Wrapped())
			})
		}
	}, func() {
		s.modeSelect(c, idx, it)
		s.recorders[idx].declare(l, c.Key("recorder"), locales.SettingsShortcutInputLabel(label), s.accelerators[it.action], func(accel string) {
			s.change(it.action, accel)
		})
		s.clearSlot(c, it, label)
	})
}

// modeSelect 是「全局 / 局部」下拉：外层 <div class="w-[110px]">，className 的 h-9 text-xs 落在 Select 的容器上，
// 触发按钮自己仍是 w-full h-10 text-sm，所以按钮（40px）比容器（36px）高，向下溢出 4px；容器让一行的对齐按 36px 算。
// 按钮没有 aria-label，可访问名取按钮里的文字（当前模式的名称）。
func (s *Shortcuts) modeSelect(c ui.C, idx int, it shortcutItem) {
	l := s.Look
	options := []kit.SelectOption{
		{Value: services.ShortcutModeGlobal, Label: locales.SettingsShortcutModeGlobal()},
		{Value: services.ShortcutModeLocal, Label: locales.SettingsShortcutModeLocal()},
	}
	current := ""
	for _, o := range options {
		if o.Value == s.modes[it.action] {
			current = o.Label
		}
	}
	ui.Column(c.Key("mode-box")).Width(110).Height(l.Rem(2.25)).Shrink(0).Children(func() {
		picked, changed := l.Select(c.Key("mode"), &s.modeSels[idx], kit.SelectSpec{
			Label: current, Value: s.modes[it.action], MenuMinWidth: 110, Width: 110, Options: options,
		})
		if changed {
			s.setMode(it.action, picked)
		}
	})
}

// clearSlot 是 w-8 的清除按钮位：有快捷键才显示（p-2 圆形图标按钮，悬停 bg-muted、text-destructive）。
func (s *Shortcuts) clearSlot(c ui.C, it shortcutItem, label string) {
	l := s.Look
	t := l.T
	slot := ui.Row(c.Key("clear-slot")).Width(l.Rem(2)).Shrink(0).Height(l.Rem(2)).Center()
	slot.Children(func() {
		if s.accelerators[it.action] == "" {
			return
		}
		name := locales.SettingsClearShortcutFor(label)
		size := l.Rem(2)
		cc := c.Key("clear")
		btn := ui.Row(cc).Size(size, size).Center().Radius(size/2).
			HitWith(ui.ID(cc), name, hitButton)
		ink := t.MutedForeground
		if btn.State().Hovered {
			ink = t.Destructive
			btn = btn.Background(l.Col(t.Muted))
		}
		btn.Children(func() { l.Icon(c.Key("icon"), icons.X, l.Rem(1), ink) })
		if btn.Clicked() {
			s.clear(it.action)
		}
	})
}

// labelOf 是动作的显示名；未知动作原样返回。
func (s *Shortcuts) labelOf(action string) string {
	for _, it := range shortcutItems {
		if it.action == action {
			return it.label()
		}
	}
	return action
}

// conflictWith 返回已被别的动作占用该快捷键的动作（渲染层 checkShortcutConflict：按动作表顺序取第一个）。
func (s *Shortcuts) conflictWith(action, accel string) string {
	for _, it := range shortcutItems {
		if it.action != action && s.accelerators[it.action] == accel {
			return it.action
		}
	}
	return ""
}

// change 处理录到的新快捷键：与别的动作冲突时拒绝并标出冲突项，否则保存。
func (s *Shortcuts) change(action, accel string) {
	if other := s.conflictWith(action, accel); other != "" {
		s.conflicts[action] = other
		s.Guard.Reject(locales.SettingsShortcutConflict(s.labelOf(other)))
		return
	}
	delete(s.conflicts, action)
	s.persist(action, accel, locales.SettingsShortcutUpdated())
}

// clear 清除一项快捷键（保存空串）。
func (s *Shortcuts) clear(action string) {
	delete(s.conflicts, action)
	s.persist(action, "", locales.SettingsShortcutCleared())
}

// persist 把一项改动连同其余快捷键交给服务保存（渲染层 persistShortcuts）：
// 系统拒绝注册（被其他程序占用）与保存出错都回到原值并弹出带原因的错误提示，成功才弹「已更新」。
func (s *Shortcuts) persist(action, accel, success string) {
	next := cloneStrings(s.accelerators)
	next[action] = accel
	var err error
	if s.Service == nil {
		err = errShortcutsUnavailable
	} else {
		err = s.Service.Save(context.Background(), next)
	}
	if err != nil {
		if errors.Is(err, services.ErrShortcutNotSaved) {
			err = errors.New(locales.SettingsShortcutRegisterFailed())
		}
		s.Guard.Fail(locales.SettingsShortcutSaveFailed(), locales.SettingsShortcutSaveFailed(), err)
		return
	}
	s.accelerators = next
	s.Guard.Succeed(success)
}

// setMode 保存一项的触发范围（渲染层 setShortcutMode：连同其余项的范围一起写回）。
// 写入失败时范围保持原值并提示。
func (s *Shortcuts) setMode(action, mode string) {
	next := cloneStrings(s.modes)
	next[action] = mode
	ok := s.Guard.Run(locales.SettingsShortcutMode(), func() error {
		if s.Service == nil {
			return errShortcutsUnavailable
		}
		return s.Service.SetModes(context.Background(), next)
	})
	if ok {
		s.modes = next
	}
}

func cloneStrings(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
