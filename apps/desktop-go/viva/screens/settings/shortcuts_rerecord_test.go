package settings_test

import (
	"testing"

	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 把一项录成它自己已经是的组合键，不是冲突：冲突只看「别的动作」占用。
// 若把自己也算进去，用户重录同一个键（比如想确认或刷新注册）会被误报「与自己冲突」而拒绝。
func TestShortcutsRerecordOwnCombinationIsNotConflict(t *testing.T) {
	svc := &servicestest.Shortcuts{}
	env := newEnv()
	r := openShortcuts(t, svc, env)

	r.startRecording(showAppLabel)
	r.keys("alt+shift+p") // 显示窗口的默认组合键
	if errs := env.toasts(kit.ToastError); len(errs) != 0 {
		t.Fatalf("重录自己的组合键不应报错，得到 %+v", errs)
	}
	if r.hasText(locales.SettingsConflictWith(showAppLabel)) {
		t.Error("不应标出「与自己冲突」")
	}
	saves := svc.Saves()
	if len(saves) != 1 || saves[0]["showApp"] != "Alt+Shift+P" || saves[0]["newItem"] != "Alt+Shift+N" {
		t.Errorf("应照常保存一次且其余项不变，得到 %v", saves)
	}
	if ok := env.toasts(kit.ToastSuccess); len(ok) != 1 || ok[0].Message != locales.SettingsShortcutUpdated() {
		t.Errorf("应弹「快捷键已更新」，得到 %+v", ok)
	}
}
