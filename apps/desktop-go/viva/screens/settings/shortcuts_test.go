package settings_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	sscreen "guizhi-mygo-shell/viva/screens/settings"
)

// openShortcuts 打开「快捷键」页；svc 为 nil 表示外壳没接快捷键服务。
func openShortcuts(t *testing.T, svc *servicestest.Shortcuts, env *sysEnv) *rig {
	t.Helper()
	return openPage(t, func(look *kit.Look) func(c ui.C) {
		s := &sscreen.Shortcuts{Look: look, Guard: env.guard(), Wake: func() {}}
		if svc != nil {
			s.Service = svc
		}
		t.Cleanup(s.Close)
		return s.Page
	})
}

// modeBox 取第 idx 个（按阅读顺序，与 shortcutItems 同序）触发范围下拉。下拉按钮没有 aria-label，
// 可访问名取按钮里的当前模式文字，四个同名且随值变化，所以只能按顺序找。
func (r *rig) modeBox(idx int) app.AccessNode {
	r.t.Helper()
	var boxes []app.AccessNode
	for _, n := range r.s.Access() {
		if n.Role == gfx.RoleCombobox {
			boxes = append(boxes, n)
		}
	}
	if idx >= len(boxes) {
		r.t.Fatalf("只有 %d 个下拉，取不到第 %d 个", len(boxes), idx)
	}
	return boxes[idx]
}

// pickMode 展开第 idx 个触发范围下拉并选中某一项。
func (r *rig) pickMode(idx int, option string) {
	r.t.Helper()
	b := r.modeBox(idx).Bounds
	if err := r.s.Click(b.X+b.Width/2, b.Y+b.Height/2); err != nil {
		r.t.Fatalf("展开下拉失败：%v", err)
	}
	r.frames(3)
	r.clickNode(gfx.RoleOption, option)
}

// 录制框的可访问名与说明里用到的标签。
var (
	showAppLabel = locales.SettingsShortcutShowApp()
	newItemLabel = locales.SettingsShortcutNewItem()
)

// startRecording 点录制框开始录制，并确认进入了「请按下快捷键」。
func (r *rig) startRecording(label string) {
	r.t.Helper()
	r.clickNode(gfx.RoleGroup, locales.SettingsShortcutInputLabel(label))
	if !r.hasText(locales.SettingsPressKeys()) {
		r.t.Fatalf("点「%s」的录制框后应进入录制状态", label)
	}
}

func TestShortcutsShowDefaultsAndTips(t *testing.T) {
	r := openShortcuts(t, &servicestest.Shortcuts{}, newEnv())
	for _, want := range []string{"Alt + Shift + P", "Alt + Shift + N", "Alt + Shift + F", "Alt + Shift + S",
		locales.SettingsShortcutTip1(), locales.SettingsShortcutTip3(), locales.SettingsShortcutModeDesc()} {
		if !r.hasText(want) {
			t.Errorf("页面上应有 %q", want)
		}
	}
}

// 加载服务里保存的配置：组合键与各项的触发范围。
func TestShortcutsLoadsSavedConfig(t *testing.T) {
	svc := &servicestest.Shortcuts{}
	svc.Set(services.ShortcutConfig{
		Accelerators: map[string]string{"showApp": "CommandOrControl+Alt+K", "newItem": "", "search": "Alt+Shift+F", "settings": "Alt+Shift+S"},
		Modes:        map[string]string{"showApp": "local", "newItem": "global"},
	})
	r := openShortcuts(t, svc, newEnv())
	waitUntil(r, "已保存的组合键", func() bool { return r.hasText("Ctrl + Alt + K") })
	if !r.hasText(locales.SettingsNotSet()) {
		t.Error("清空的快捷键应显示「未设置」")
	}
	if n := r.modeBox(1); n.Value != locales.SettingsShortcutModeGlobal() {
		t.Errorf("新建条目的触发范围应是全局，得到 %q", n.Value)
	}
	if n := r.modeBox(0); n.Value != locales.SettingsShortcutModeLocal() {
		t.Errorf("显示窗口的触发范围应是应用内，得到 %q", n.Value)
	}
}

// 读取失败：弹出带原因的错误提示，界面保持默认值（不渲染成空白）。
func TestShortcutsLoadFailureShowsDetail(t *testing.T) {
	svc := &servicestest.Shortcuts{}
	svc.FailGet(errors.New("config unreadable"))
	env := newEnv()
	r := openShortcuts(t, svc, env)
	waitUntil(r, "加载失败提示", func() bool { return len(env.toasts(kit.ToastError)) > 0 })
	errs := env.toasts(kit.ToastError)
	if len(errs) != 1 || errs[0].Message != locales.SettingsShortcutLoadFailed() || !strings.Contains(errs[0].Detail, "config unreadable") {
		t.Errorf("应弹「加载失败」并带原因，得到 %+v", errs)
	}
	if !r.hasText("Alt + Shift + P") {
		t.Error("加载失败时应保持默认快捷键")
	}
}

// 点录制框 → 按下组合键 → 保存并提示；Ctrl 与 Alt 组合走快捷键通道，写成 CommandOrControl。
func TestShortcutsRecordSavesCombination(t *testing.T) {
	svc := &servicestest.Shortcuts{}
	env := newEnv()
	r := openShortcuts(t, svc, env)

	r.startRecording(showAppLabel)
	r.keys("alt+shift+k")
	saves := svc.Saves()
	if len(saves) != 1 || saves[0]["showApp"] != "Alt+Shift+K" || saves[0]["newItem"] != "Alt+Shift+N" {
		t.Fatalf("应保存一次，且其余项不变，得到 %v", saves)
	}
	if ok := env.toasts(kit.ToastSuccess); len(ok) != 1 || ok[0].Message != locales.SettingsShortcutUpdated() {
		t.Errorf("应弹「快捷键已更新」，得到 %+v", ok)
	}
	if !r.hasText("Alt + Shift + K") || r.hasText(locales.SettingsPressKeys()) {
		t.Error("保存后应显示新组合键并退出录制状态")
	}

	r.startRecording(showAppLabel)
	r.keys("ctrl+alt+j")
	if got := svc.Current().Accelerators["showApp"]; got != "CommandOrControl+Alt+J" {
		t.Errorf("Ctrl+Alt+J 应记为 CommandOrControl+Alt+J，得到 %q", got)
	}
	if !r.hasText("Ctrl + Alt + J") {
		t.Error("应显示 Ctrl + Alt + J")
	}
}

// 只按 Shift 加字母也算完整组合（渲染层规则：修饰键加普通键即完成）；单独一个普通键只显示、不保存。
func TestShortcutsPlainAndShiftKeys(t *testing.T) {
	svc := &servicestest.Shortcuts{}
	r := openShortcuts(t, svc, newEnv())

	r.startRecording(showAppLabel)
	r.keys("f")
	if len(svc.Saves()) != 0 {
		t.Fatal("单独一个普通键不应保存")
	}
	if r.hasText(locales.SettingsPressKeys()) {
		t.Error("按下一个键后应显示这个键，而不是提示文字")
	}
	r.keys("shift+f")
	if got := svc.Current().Accelerators["showApp"]; got != "Shift+F" {
		t.Errorf("Shift+F 应保存为 Shift+F，得到 %q", got)
	}
}

// 与别的动作冲突：拒绝保存、标出冲突项并弹错误提示；之后成功修改会清掉冲突提示。
func TestShortcutsConflictRejectedAndCleared(t *testing.T) {
	svc := &servicestest.Shortcuts{}
	env := newEnv()
	r := openShortcuts(t, svc, env)

	r.startRecording(showAppLabel)
	r.keys("alt+shift+n") // 新建条目已占用
	if len(svc.Saves()) != 0 {
		t.Fatal("冲突的组合键不应保存")
	}
	errs := env.toasts(kit.ToastError)
	if len(errs) != 1 || errs[0].Message != locales.SettingsShortcutConflict(newItemLabel) {
		t.Errorf("应弹冲突提示并点名冲突项，得到 %+v", errs)
	}
	if !r.hasText(locales.SettingsConflictWith(newItemLabel)) {
		t.Error("该行应显示「与「新建条目」冲突」")
	}
	if !r.hasText("Alt + Shift + P") {
		t.Error("冲突后显示的仍应是原来的组合键")
	}

	r.startRecording(showAppLabel)
	r.keys("alt+shift+k")
	if r.hasText(locales.SettingsConflictWith(newItemLabel)) {
		t.Error("成功修改后冲突提示应消失")
	}
}

// Esc 取消录制：不保存、退出录制状态、显示原值。
func TestShortcutsEscapeCancelsRecording(t *testing.T) {
	svc := &servicestest.Shortcuts{}
	r := openShortcuts(t, svc, newEnv())
	r.startRecording(showAppLabel)
	r.keys("escape")
	if r.hasText(locales.SettingsPressKeys()) {
		t.Error("Esc 后应退出录制状态")
	}
	if len(svc.Saves()) != 0 || !r.hasText("Alt + Shift + P") {
		t.Errorf("Esc 不应保存，原值应保留：保存 %v", svc.Saves())
	}
	// 退出后再点一下可以重新开始录制（焦点没动，也要能重来）。
	r.clickNode(gfx.RoleGroup, locales.SettingsShortcutInputLabel(showAppLabel))
	if !r.hasText(locales.SettingsPressKeys()) {
		t.Error("Esc 取消后再点录制框应重新开始录制")
	}
	r.keys("alt+shift+k")
	if got := svc.Current().Accelerators["showApp"]; got != "Alt+Shift+K" {
		t.Errorf("重新录制后应保存，得到 %q", got)
	}
}

// 失焦取消录制：点别的录制框，前一个回到原值，后一个进入录制。
func TestShortcutsBlurCancelsRecording(t *testing.T) {
	svc := &servicestest.Shortcuts{}
	r := openShortcuts(t, svc, newEnv())
	r.startRecording(showAppLabel)
	r.clickNode(gfx.RoleGroup, locales.SettingsShortcutInputLabel(newItemLabel))
	if !r.hasText("Alt + Shift + P") {
		t.Error("失焦后第一个录制框应显示原来的组合键")
	}
	r.keys("alt+shift+k")
	saves := svc.Saves()
	if len(saves) != 1 || saves[0]["newItem"] != "Alt+Shift+K" || saves[0]["showApp"] != "Alt+Shift+P" {
		t.Errorf("后一个录制框应得到组合键，前一个不变，得到 %v", saves)
	}
}

// 键盘可达：Tab 走到录制框即开始录制；Tab 离开即取消。
func TestShortcutsKeyboardReachable(t *testing.T) {
	r := openShortcuts(t, &servicestest.Shortcuts{}, newEnv())
	r.focusByTab(gfx.RoleGroup, locales.SettingsShortcutInputLabel(showAppLabel), 30)
	if !r.hasText(locales.SettingsPressKeys()) {
		t.Error("Tab 聚焦录制框后应进入录制状态")
	}
	r.keys("tab")
	if r.hasText(locales.SettingsPressKeys()) {
		t.Error("Tab 离开后应取消录制")
	}
}

// 清除：保存空串、提示「已清除」、显示「未设置」且清除按钮消失。
func TestShortcutsClear(t *testing.T) {
	svc := &servicestest.Shortcuts{}
	env := newEnv()
	r := openShortcuts(t, svc, env)
	r.clickNode(gfx.RoleButton, locales.SettingsClearShortcutFor(showAppLabel))
	saves := svc.Saves()
	if len(saves) != 1 || saves[0]["showApp"] != "" || saves[0]["newItem"] != "Alt+Shift+N" {
		t.Fatalf("清除应保存一次空串，得到 %v", saves)
	}
	if ok := env.toasts(kit.ToastSuccess); len(ok) != 1 || ok[0].Message != locales.SettingsShortcutCleared() {
		t.Errorf("应弹「快捷键已清除」，得到 %+v", ok)
	}
	if !r.hasText(locales.SettingsNotSet()) || r.has(gfx.RoleButton, locales.SettingsClearShortcutFor(showAppLabel)) {
		t.Error("清除后应显示「未设置」且没有清除按钮")
	}
}

// 保存失败：系统拒绝注册（被占用）与保存出错都回到原值并弹出带原因的提示，不弹成功。
func TestShortcutsSaveFailures(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		detail string
	}{
		{"系统拒绝注册", services.ErrShortcutNotSaved, locales.SettingsShortcutRegisterFailed()},
		{"写入出错", errors.New("disk full"), "disk full"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &servicestest.Shortcuts{}
			svc.FailSave(tc.err)
			env := newEnv()
			r := openShortcuts(t, svc, env)
			r.startRecording(showAppLabel)
			r.keys("alt+shift+k")
			errs := env.toasts(kit.ToastError)
			if len(errs) != 1 || errs[0].Message != locales.SettingsShortcutSaveFailed() || !strings.Contains(errs[0].Detail, tc.detail) {
				t.Fatalf("应弹「保存失败」并带原因 %q，得到 %+v", tc.detail, errs)
			}
			if len(env.toasts(kit.ToastSuccess)) != 0 {
				t.Error("失败时不能弹成功提示")
			}
			if !r.hasText("Alt + Shift + P") || r.hasText("Alt + Shift + K") {
				t.Error("失败后应回到原来的组合键")
			}
			if got := env.logs.all(); len(got) != 1 || !strings.Contains(got[0], tc.detail) {
				t.Errorf("失败应记一条带原因的日志，得到 %q", got)
			}
		})
	}
}

// 外壳没接快捷键服务：如实报错，不弹「已更新」。
func TestShortcutsWithoutServiceReportsFailure(t *testing.T) {
	env := newEnv()
	r := openShortcuts(t, nil, env)
	r.startRecording(showAppLabel)
	r.keys("alt+shift+k")
	if len(env.toasts(kit.ToastError)) != 1 || len(env.toasts(kit.ToastSuccess)) != 0 {
		t.Errorf("没有服务时应只弹错误提示：%+v", env.notify.Queued())
	}
}

// 触发范围下拉：改一项连同其余项的范围一起写回；写入失败时保持原值并提示。
func TestShortcutsModeChange(t *testing.T) {
	svc := &servicestest.Shortcuts{}
	env := newEnv()
	r := openShortcuts(t, svc, env)
	const idx = 1 // 新建知识条目

	svc.FailModes(errors.New("registry busy"))
	r.pickMode(idx, locales.SettingsShortcutModeGlobal())
	if n := r.modeBox(idx); n.Value != locales.SettingsShortcutModeLocal() {
		t.Errorf("写入失败时范围应保持「应用内」，得到 %q", n.Value)
	}
	if errs := env.toasts(kit.ToastError); len(errs) != 1 || !strings.Contains(errs[0].Detail, "registry busy") {
		t.Fatalf("应弹带原因的错误提示，得到 %+v", errs)
	}

	svc.FailModes(nil)
	r.pickMode(idx, locales.SettingsShortcutModeGlobal())
	sets := svc.ModeSets()
	if len(sets) != 1 || sets[0]["newItem"] != "global" || sets[0]["showApp"] != "global" || sets[0]["search"] != "local" {
		t.Errorf("应写回全部四项的范围，得到 %v", sets)
	}
	if n := r.modeBox(idx); n.Value != locales.SettingsShortcutModeGlobal() {
		t.Errorf("成功后范围应是「全局」，得到 %q", n.Value)
	}
}

// gatedShortcuts 让 Get 先取下当时的配置、再卡在 gate 上：模拟「首轮读取很慢，旧结果在用户保存之后才送达」。
type gatedShortcuts struct {
	*servicestest.Shortcuts
	entered, gate, returned chan struct{}
}

func (g *gatedShortcuts) Get(ctx context.Context) (services.ShortcutConfig, error) {
	cfg, err := g.Shortcuts.Get(ctx)
	close(g.entered)
	<-g.gate
	defer close(g.returned)
	return cfg, err
}

// 首轮读取比用户的保存慢：旧读取结果送达后，不能把刚清除的快捷键与刚改的触发范围盖回旧值。
func TestShortcutsStaleLoadDoesNotOverwriteLocalEdit(t *testing.T) {
	inner := &servicestest.Shortcuts{}
	g := &gatedShortcuts{Shortcuts: inner, entered: make(chan struct{}), gate: make(chan struct{}), returned: make(chan struct{})}
	var once sync.Once
	release := func() { once.Do(func() { close(g.gate) }) }
	t.Cleanup(release) // 失败提前退出时也要放行，免得后台 goroutine 泄漏

	env := newEnv()
	r := openPage(t, func(look *kit.Look) func(c ui.C) {
		s := &sscreen.Shortcuts{Look: look, Guard: env.guard(), Wake: func() {}, Service: g}
		t.Cleanup(s.Close)
		return s.Page
	})
	<-g.entered // 首轮读取已取走旧配置，但还没送回界面

	r.clickNode(gfx.RoleButton, locales.SettingsClearShortcutFor(showAppLabel))
	r.pickMode(1, locales.SettingsShortcutModeGlobal())
	if len(inner.Saves()) != 1 || len(inner.ModeSets()) != 1 {
		t.Fatalf("应各保存一次，得到 %v / %v", inner.Saves(), inner.ModeSets())
	}

	release()
	<-g.returned
	for i := 0; i < 20; i++ { // 旧结果经邮箱回到界面线程，给它足够的帧
		r.frames(1)
		time.Sleep(5 * time.Millisecond)
	}
	if !r.hasText(locales.SettingsNotSet()) || r.has(gfx.RoleButton, locales.SettingsClearShortcutFor(showAppLabel)) {
		t.Error("旧读取结果不应把已清除的快捷键盖回去")
	}
	if n := r.modeBox(1); n.Value != locales.SettingsShortcutModeGlobal() {
		t.Errorf("旧读取结果不应把已改的触发范围盖回去，得到 %q", n.Value)
	}
}
