package themed

import (
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 联网搜索设置（ReadingSearchSettings.tsx）：设置页 AI 分区里的一块。状态在 SearchSettings（search.go）。
// API Key 明文只在输入框与 Store 内存里：不进日志、不进快照、不进任何提示文字；设置页离开这一块（或切走）就清空。

// searchUI 是这一块的界面状态。
type searchUI struct {
	shown, mounted bool
	key            ui.Field
	showKey        bool
	// synced 是上一次交给 Store 的密钥：Store 在保存成功后清空了密钥时，据此把输入框一并清空。
	synced   string
	provider kit.SelectState
}

// SearchPanel 画「联网搜索」设置块（与 AI 分区其余块同宽）。每一帧由设置页在 AI 分区里调用。
//
// 版面照 ReadingSearchSettings 的 section：`space-y-3 rounded-xl border border-border p-4`，不是设置页通用的
// 「卡片外标题」——标题 h3（text-base font-medium，前景色）与说明（text-sm 弱化）都在边框里面，没有卡片底色。
// 1920×1080 对拍实测：React 外框 [248,851]、标题 [265,868,1302×24]、说明 [265,904,1302×20]、默认开关行 [265,936]（含 border-b）。
func (s *Screen) SearchPanel(c ui.C) {
	l := s.Look
	u := &s.search
	u.shown = true
	if !u.mounted {
		u.mounted = true
		u.key.Secure = true
		s.spawn(func() { s.Search.Load() })
	}
	v := s.Search.View()
	if !v.KeyFilled && u.synced != "" {
		u.key.SetText("")
		u.synced = ""
	}
	c = c.Key("themed-search")
	t := l.T
	frame := kit.Surface{Border: t.Border, BorderW: 1, Radius: l.Rem(0.75)} // rounded-xl border border-border，无底色
	ui.Column(c.Key("section")).FillWidth().Pad(1).Draw(func(d *ui.DrawContext) {
		l.PaintSurface(d, frame)
	}).Children(func() {
		ui.Column(c.Key("body")).FillWidth().Gap(l.Space(3)).Pad(l.Space(4)).Children(func() {
			l.Heading(c.Key("title"), 3, locales.SettingsReadingSearchTitle(), kit.TextBase, kit.WeightMedium, t.Foreground)
			s.bodyText(c.Key("desc"), locales.SettingsReadingSearchDesc(), t.MutedForeground)
			if v.Status == nil {
				s.searchLoading(c.Key("loading"), v)
				return
			}
			s.searchForm(c.Key("form"), v)
			if v.Error != "" {
				s.alertLine(c.Key("error"), v.Error)
			}
			s.searchButtons(c.Key("buttons"), v)
			if v.Busy != "" {
				s.statusLine(c.Key("busy"), v.Busy)
			}
		})
	})
}

// searchLoading 是还没读到配置：读取中的提示，或失败原因加「重试读取」。
func (s *Screen) searchLoading(c ui.C, v SearchView) {
	l := s.Look
	switch {
	case v.Error != "":
		s.alertLine(c.Key("error"), v.Error)
		spec := kit.ButtonSpec{Label: locales.LibrarySnapshotRetryLoad(), Disabled: v.Busy != ""}
		if l.Button(c.Key("retry"), spec, s.compactButton()).Clicked() {
			s.spawn(func() { s.Search.Load() })
		}
	default:
		s.statusLine(c.Key("text"), locales.SettingsReadingSearchLoading())
	}
}

// searchForm 是默认开关、服务选择、密钥（或原生搜索的说明）与状态行。
func (s *Screen) searchForm(c ui.C, v SearchView) {
	l := s.Look
	t := l.T
	u := &s.search
	busy := v.Busy != ""

	// 生成阅读页时默认联网搜索：开关一拨动就保存。
	// 渲染层用的是设置页共用的 SettingItem（px-4 py-3 加 border-b，不是最后一项），这里同样用 kit.SettingItem。
	l.SettingItem(c.Key("default"), kit.SettingRow{
		Label:       locales.SettingsReadingSearchDefault(),
		Description: locales.SettingsReadingSearchDefaultDesc(),
		Control: func() {
			on := v.Status.DefaultEnabled
			// 以 Clicked 为准而不是比较 on：on 是每帧新建的局部变量，Viva 的写回落在上一帧那一份上。
			if l.SwitchWith(c.Key("switch"), &on, kit.SwitchSpec{Label: locales.SettingsReadingSearchDefault(), Disabled: busy}).Clicked() {
				want := !v.Status.DefaultEnabled
				s.spawn(func() { s.Search.SetDefault(want) })
			}
		},
	}, false)

	// 搜索服务。
	ui.Column(c.Key("provider")).FillWidth().Gap(l.Space(1.5)).Children(func() {
		l.Text(c.Key("label"), locales.SettingsReadingSearchProvider(), kit.TextSM, kit.WeightNormal, t.Foreground)
		opts := []kit.SelectOption{
			{Value: ProviderNative, Label: ProviderName(ProviderNative)},
			{Value: ProviderTavily, Label: ProviderName(ProviderTavily)},
			{Value: ProviderAnySearch, Label: ProviderName(ProviderAnySearch)},
		}
		if picked, changed := l.Select(c.Key("select"), &u.provider, kit.SelectSpec{
			Label: locales.SettingsReadingSearchProvider(), Value: v.Provider, Options: opts, Width: -1,
		}); changed && !busy {
			s.Search.SetProvider(picked)
			u.key.SetText("")
			u.synced = ""
		}
	})

	if v.Native() {
		ui.Column(c.Key("native")).FillWidth().Gap(l.Space(1)).Children(func() {
			s.bodyText(c.Key("info"), v.NativeInfo(), t.MutedForeground)
			s.bodyText(c.Key("hint"), locales.SettingsReadingSearchNativeHint(), t.MutedForeground)
		})
	} else {
		s.keyInput(c.Key("key"), v)
	}
	s.smallText(c.Key("status"), v.StatusLine())
}

// keyInput 是 API Key 输入框：口令样式（默认圆点），有显示 / 隐藏钮；每次编辑把明文交给 Store。
func (s *Screen) keyInput(c ui.C, v SearchView) {
	l := s.Look
	t := l.T
	u := &s.search
	name := ProviderName(v.Provider) + " API Key"
	// 渲染层是 `<label className="block text-sm">API Key<input className="mt-1 ...">`：标签与输入框之间 mt-1（4px）。
	ui.Column(c).FillWidth().Gap(l.Space(1)).Children(func() {
		l.Text(c.Key("label"), "API Key", kit.TextSM, kit.WeightNormal, t.Foreground)
		box := l.SettingsPasswordInput(c.Key("field"), &u.key, &u.showKey, kit.SettingsInputSpec{
			Name: name, Placeholder: v.KeyPlaceholder(),
		}, locales.CommonShowPassword(), locales.CommonHidePassword())
		if box.Changed() && v.Busy == "" {
			text := u.key.Text()
			s.Search.SetKey(text)
			u.synced = text
		}
	})
}

// searchButtons 是保存、测试连接、清除密钥（原生搜索没有密钥，不出现清除）。
func (s *Screen) searchButtons(c ui.C, v SearchView) {
	l := s.Look
	ui.Flow(c).FillWidth().Gap(l.Space(2)).LineGap(l.Space(2)).Children(func() {
		if l.Button(c.Key("save"), kit.ButtonSpec{Label: locales.CommonSave(), Disabled: !v.CanSave()}, s.compactPrimary()).Clicked() {
			s.spawn(func() { s.Search.Save() })
		}
		if l.Button(c.Key("test"), kit.ButtonSpec{Label: locales.SettingsTestConnection(), Disabled: !v.CanTest()}, s.compactButton()).Clicked() {
			s.spawn(func() { s.Search.Test() })
		}
		if !v.Native() {
			spec := kit.ButtonSpec{Label: locales.SettingsReadingSearchClearKey(), Disabled: !v.CanClear()}
			if l.Button(c.Key("clear"), spec, s.compactButton()).Clicked() {
				s.spawn(func() { s.Search.ClearKey() })
			}
		}
	})
}

// endSearch 在每一帧结束时检查：这一帧没画搜索设置块（切到别的分区或页面），就清掉输入框里的密钥并复位，下次重新读取。
func (s *Screen) endSearch() {
	u := &s.search
	if !u.shown && u.mounted {
		u.mounted = false
		u.key.SetText("")
		u.showKey = false
		u.synced = ""
		s.Search.SetKey("")
	}
	u.shown = false
}
