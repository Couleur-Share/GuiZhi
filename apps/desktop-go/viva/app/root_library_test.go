package app

import (
	"testing"

	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/native/settings"
)

// 知识库详情的自动保存开关来自设置（渲染层 useSettingsStore 的 autoSave）：每次现读，设置在别处改了立刻生效；
// 默认开着。Store.AutoSave 没接线时 AutoSaveEnabled 也是开着，所以关掉之后必须跟着变才算接上了线。
func TestLibraryAutoSaveFollowsTheSetting(t *testing.T) {
	f := openRoot(t, func(o *Options) { o.Services.Library = &servicestest.Library{} })
	store := f.root.library.Store
	if !store.AutoSaveEnabled() {
		t.Fatal("默认自动保存开着")
	}
	if err := f.root.store.Update(func(v *settings.Values) { v.UI.AutoSave = false }); err != nil {
		t.Fatal(err)
	}
	if store.AutoSaveEnabled() {
		t.Fatal("设置里关掉之后，知识库应跟着不再自动保存")
	}
	if err := f.root.store.Update(func(v *settings.Values) { v.UI.AutoSave = true }); err != nil {
		t.Fatal(err)
	}
	if !store.AutoSaveEnabled() {
		t.Fatal("设置里重新打开之后应恢复")
	}
}
