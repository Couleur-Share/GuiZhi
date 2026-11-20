package app

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/screens/shell"
)

// 界面语言（台账 B-049）：设置里的语言是进程级 locales 状态的唯一来源。
// 首次读取、别处改动（订阅）、「重试」与写入成功都要接过去；写入失败时设置不变，语言也不变。

// langAtStart 把进程语言设成 l 作为测试的起点，结束时还原成中文（其余测试默认中文）。
func langAtStart(t *testing.T, l locales.Lang) {
	t.Helper()
	locales.SetLang(l)
	t.Cleanup(func() { locales.SetLang(locales.Zh) })
}

func wantLang(t *testing.T, what string, want locales.Lang) {
	t.Helper()
	if got := locales.CurrentLang(); got != want {
		t.Errorf("%s：界面语言 = %v，期望 %v", what, got, want)
	}
}

func setLanguage(l settings.Language) func(*settings.Values) {
	return func(v *settings.Values) { v.App.Language = l }
}

// 保存的语言在启动时被采用：这是 B-049 的原始症状（保存的 en 下次启动仍是中文）。
func TestSettingsStoreAppliesSavedLanguageAtStart(t *testing.T) {
	svc := &servicestest.Settings{}
	if _, err := svc.Update(setLanguage(settings.LangEn)); err != nil {
		t.Fatal(err)
	}
	langAtStart(t, locales.Zh)
	store := newSettingsStore(svc, nil)
	defer store.Close()
	wantLang(t, "保存的是英文，启动读取之后", locales.En)

	// 反过来：保存的是中文，进程里残留英文时，启动读取之后回到中文（设置是唯一来源，不是谁先设谁说了算）。
	svc2 := &servicestest.Settings{}
	langAtStart(t, locales.En)
	store2 := newSettingsStore(svc2, nil)
	defer store2.Close()
	wantLang(t, "保存的是中文（默认），启动读取之后", locales.Zh)
}

// 没有服务（测试里不关心设置）时用默认值，语言是中文。
func TestSettingsStoreWithoutServiceUsesDefaultLanguage(t *testing.T) {
	langAtStart(t, locales.En)
	store := newSettingsStore(nil, nil)
	defer store.Close()
	wantLang(t, "没有服务", locales.Zh)
	if err := store.Update(setLanguage(settings.LangEn)); err != nil {
		t.Fatal(err)
	}
	wantLang(t, "没有服务时在内存里改成英文", locales.En)
}

// 写入成功就换语言；写入失败时缓存与语言都不变。
func TestSettingsStoreUpdateAppliesLanguage(t *testing.T) {
	langAtStart(t, locales.Zh)
	svc := &servicestest.Settings{}
	store := newSettingsStore(svc, nil)
	defer store.Close()

	if err := store.Update(setLanguage(settings.LangEn)); err != nil {
		t.Fatal(err)
	}
	wantLang(t, "写入英文成功", locales.En)

	svc.FailWith(errors.New("磁盘已满"))
	if err := store.Update(setLanguage(settings.LangZh)); err == nil {
		t.Fatal("服务写入失败时 Update 应返回错误")
	}
	wantLang(t, "写回中文失败", locales.En)
	if store.Values().App.Language != settings.LangEn {
		t.Error("写入失败后缓存里的语言不应改变")
	}

	svc.FailWith(nil)
	if err := store.Update(setLanguage(settings.LangZh)); err != nil {
		t.Fatal(err)
	}
	wantLang(t, "重试写回中文成功", locales.Zh)
}

// quietSettings 的订阅永远收不到通知：用来确认写入方自己同步语言，而不是等订阅回调
// （生产里回调在锁外、可能晚于下一帧；选了语言要当帧生效）。
type quietSettings struct{ servicestest.Settings }

func (q *quietSettings) Subscribe(func(settings.Values)) func() { return func() {} }

func TestSettingsStoreUpdateAppliesLanguageWithoutSubscription(t *testing.T) {
	langAtStart(t, locales.Zh)
	store := newSettingsStore(&quietSettings{}, nil)
	defer store.Close()
	if err := store.Update(setLanguage(settings.LangEn)); err != nil {
		t.Fatal(err)
	}
	wantLang(t, "订阅收不到通知时写入英文", locales.En)
}

// 别处（另一个入口、导入设置）改了语言：订阅回调接过去，并请求重绘。
func TestSettingsStoreFollowsLanguageChangedElsewhere(t *testing.T) {
	langAtStart(t, locales.Zh)
	svc := &servicestest.Settings{}
	redraws := 0
	store := newSettingsStore(svc, func() { redraws++ })
	defer store.Close()

	if _, err := svc.Update(setLanguage(settings.LangEn)); err != nil {
		t.Fatal(err)
	}
	wantLang(t, "别处改成英文", locales.En)
	if redraws == 0 {
		t.Error("别处改了语言后没有请求重绘")
	}
}

// 首次读取失败时界面停在失败状态（默认语言）；「重试」成功后换成保存的语言。
func TestSettingsStoreReloadAppliesLanguage(t *testing.T) {
	langAtStart(t, locales.Zh)
	svc := &flakySettings{}
	if _, err := svc.Update(setLanguage(settings.LangEn)); err != nil {
		t.Fatal(err)
	}
	svc.failing.Store(true)
	store := newSettingsStore(svc, nil)
	defer store.Close()
	wantLang(t, "首次读取失败，停在默认语言", locales.Zh)

	if err := store.Reload(); err == nil {
		t.Fatal("仍失败时 Reload 应返回错误")
	}
	wantLang(t, "重试仍失败", locales.Zh)

	svc.failing.Store(false)
	if err := store.Reload(); err != nil {
		t.Fatalf("服务恢复后 Reload 应成功：%v", err)
	}
	wantLang(t, "重试成功", locales.En)
}

// 界面级：在设置页的语言下拉里选 English，当场整页换成英文；落盘的是 en。
// 这是 B-049 的用户路径：选了没反应，保存的 en 也不被采用。
func TestRootSwitchesLanguageLiveFromTheSettingsPage(t *testing.T) {
	langAtStart(t, locales.Zh)
	f := openRoot(t, nil)
	wantLang(t, "默认设置打开设置页", locales.Zh)
	if !f.session.HasText("应用设置") {
		t.Fatal("前提不成立：默认语言下设置页标题应是「应用设置」")
	}

	combo, ok := f.session.AccessByRole(gfx.RoleCombobox, locales.SettingsLanguage())
	if !ok {
		t.Fatalf("没有找到名为 %q 的语言下拉", locales.SettingsLanguage())
	}
	if err := f.session.ClickLabel(combo.Label); err != nil {
		t.Fatalf("点开语言下拉失败：%v", err)
	}
	f.frames(t, 3)
	if err := f.session.ClickLabel(locales.NativeLanguageNameEn()); err != nil {
		t.Fatalf("选「%s」失败：%v", locales.NativeLanguageNameEn(), err)
	}
	f.frames(t, 4)

	if got := f.fake.Values().App.Language; got != settings.LangEn {
		t.Fatalf("落盘的语言 = %q，期望 en", got)
	}
	wantLang(t, "选了 English", locales.En)
	if f.session.HasText("应用设置") {
		t.Error("选了 English 之后，页面上还留着中文标题「应用设置」")
	}
	if !f.session.HasText(locales.SettingsGeneral()) {
		t.Errorf("选了 English 之后，页面上没有英文的分区标题 %q", locales.SettingsGeneral())
	}
}

// ---- 对拍：运行中切换，与一开始就用目标语言打开的窗口逐行相同 ----
//
// 判据不是「出现了某几个英文词」：缓存了旧语言文字的行、构造时取一次就存下来的文案，都会在对拍里露出来。
// 语言是进程级状态，这些测试不并行，并且自己还原成中文。

// rootTexts 是画面上从上到下的每一行文字，连同它画在哪里（位置与大小取到 0.01 逻辑像素）。
func rootTexts(f *rootFixture) []string {
	var out []string
	for _, tx := range f.session.Snapshot().Texts {
		b := tx.Bounds
		out = append(out, fmt.Sprintf("%s @%.2f,%.2f %.2fx%.2f", tx.Value, b.X, b.Y, b.Width, b.Height))
	}
	return out
}

func sameRootTexts(t *testing.T, what string, got, want []string) {
	t.Helper()
	if slices.Equal(got, want) {
		return
	}
	var diffs []string
	for i := 0; i < max(len(got), len(want)) && len(diffs) < 8; i++ {
		var g, w string
		if i < len(got) {
			g = got[i]
		}
		if i < len(want) {
			w = want[i]
		}
		if g != w {
			diffs = append(diffs, fmt.Sprintf("第 %d 行：实际 %q，期望 %q", i, g, w))
		}
	}
	t.Errorf("%s：画面文字不一致（实际 %d 行，期望 %d 行）\n  %s", what, len(got), len(want), strings.Join(diffs, "\n  "))
}

// openRootEnglish 打开一个设置里语言就是英文的 Root：这就是「下次启动沿用保存的英文」的路径。
func openRootEnglish(t *testing.T, mutate func(*Options)) *rootFixture {
	t.Helper()
	saved := &servicestest.Settings{}
	if _, err := saved.Update(setLanguage(settings.LangEn)); err != nil {
		t.Fatal(err)
	}
	return openRoot(t, func(o *Options) {
		o.Services.Settings = saved
		if mutate != nil {
			mutate(o)
		}
	})
}

// 设置页：在 Root 里整页（外壳、侧栏、顶栏、设置内容）切到英文，与保存了英文的新窗口对拍；换回中文与最初对拍。
func TestRootLiveLanguageSwitchMatchesFreshEnglishOnSettings(t *testing.T) {
	langAtStart(t, locales.Zh)
	live := openRoot(t, nil)
	zh := rootTexts(live)

	if _, err := live.fake.Update(setLanguage(settings.LangEn)); err != nil {
		t.Fatal(err)
	}
	live.frames(t, 4)
	enLive := rootTexts(live)
	if slices.Equal(zh, enLive) {
		t.Fatal("切到英文后画面文字一行没变：语言没有传到界面")
	}
	sameRootTexts(t, "设置页切到英文，与保存了英文的新窗口对拍", enLive, rootTexts(openRootEnglish(t, nil)))

	if _, err := live.fake.Update(setLanguage(settings.LangZh)); err != nil {
		t.Fatal(err)
	}
	live.frames(t, 4)
	sameRootTexts(t, "设置页换回中文，与最初的中文画面对拍", rootTexts(live), zh)
}

// 导入队列：任务行带缓存（kit.RowEpoch.Variant 含语言），切语言后整页与新窗口一致。
func TestRootLiveLanguageSwitchMatchesFreshEnglishOnImports(t *testing.T) {
	langAtStart(t, locales.Zh)
	open := func(openFn func(*testing.T, func(*Options)) *rootFixture) *rootFixture {
		svc := &servicestest.Imports{}
		svc.Set(services.ImportPage{
			Total:   2,
			Counts:  map[string]int64{"all": 2, "completed": 2},
			Entries: []services.ImportTask{importTask("t1", "示例页面"), importTask("t2", "另一个页面")},
		}, nil)
		f := openFn(t, func(o *Options) {
			o.Services.Imports = svc
			o.Page, o.Module = shell.PageHome, shell.ModuleImports
		})
		waitUntil(t, "工作区显示读到的任务", func() bool {
			f.frames(t, 1)
			return f.session.HasText("示例页面") && f.session.HasText("另一个页面")
		})
		f.frames(t, 3)
		return f
	}

	live := open(openRoot)
	zh := rootTexts(live)
	if _, err := live.fake.Update(setLanguage(settings.LangEn)); err != nil {
		t.Fatal(err)
	}
	live.frames(t, 4)
	enLive := rootTexts(live)
	if slices.Equal(zh, enLive) {
		t.Fatal("切到英文后导入队列画面文字一行没变：语言没有传到界面")
	}
	sameRootTexts(t, "导入队列切到英文，与保存了英文的新窗口对拍", enLive, rootTexts(open(openRootEnglish)))

	if _, err := live.fake.Update(setLanguage(settings.LangZh)); err != nil {
		t.Fatal(err)
	}
	live.frames(t, 4)
	sameRootTexts(t, "导入队列换回中文，与最初的中文画面对拍", rootTexts(live), zh)
}