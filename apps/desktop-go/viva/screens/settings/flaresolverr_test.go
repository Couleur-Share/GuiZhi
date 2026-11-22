package settings_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	nsettings "guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	sscreen "guizhi-mygo-shell/viva/screens/settings"
)

// openFlareSolverr 把 FlareSolverr 块单独放进设置内容壳里打开（渲染层它属于「采集与转写」页，M3-2b 迁移时直接挂载；
// 这里的壳只是给它一个与真实页面同样的滚动容器与分区间距）。solver 为 nil 表示外壳没接 FlareSolverr 服务。
func openFlareSolverr(t *testing.T, store sscreen.Store, env *sysEnv, solver *servicestest.Solver) *rig {
	t.Helper()
	return openPage(t, func(look *kit.Look) func(c ui.C) {
		f := &sscreen.FlareSolverr{Look: look, Store: store, Guard: env.guard(), Wake: func() {}}
		if solver != nil {
			f.Solver = solver
		}
		t.Cleanup(f.Close)
		return func(c ui.C) {
			look.SettingsContent(c, "FlareSolverr", func() {
				look.SettingSections(c, func() { f.Page(c.Key("flaresolverr")) })
			})
		}
	})
}

func TestFlareSolverrSaveAndTestUseDraft(t *testing.T) {
	store := sscreen.NewMemStore(nsettings.Default())
	env := newEnv()
	solver := &servicestest.Solver{}
	r := openFlareSolverr(t, store, env, solver)
	r.scrollToEnd()

	// 编辑只改草稿：保存之前设置不变。
	r.clickNode(gfx.RoleSwitch, locales.WebCaptureSolverEnable())
	r.typeInto(locales.WebCaptureSolverPort(), "9100")
	if store.Updates != 0 {
		t.Fatalf("保存之前不应写入设置，已写入 %d 次", store.Updates)
	}

	// 测试连接用的是草稿（还没保存的端口），且不改设置。
	r.clickNode(gfx.RoleButton, locales.WebCaptureSolverCheck())
	waitUntil(r, "连接成功提示", func() bool { r.scrollToEnd(); return r.hasText(locales.WebCaptureSolverConnected()) })
	calls := solver.Calls()
	if len(calls) != 1 || calls[0].Port != 9100 || !calls[0].Enabled {
		t.Errorf("测试连接应使用草稿配置，得到 %+v", calls)
	}
	if store.Updates != 0 {
		t.Error("测试连接不应改动已保存的设置")
	}

	r.clickNode(gfx.RoleButton, locales.WebCaptureSolverSave())
	got := store.Values().App.FlareSolverr
	if !got.Enabled || got.Port != 9100 {
		t.Errorf("保存后设置 = %+v", got)
	}
	r.scrollToEnd()
	if !r.hasText(locales.WebCaptureSolverSaved()) {
		t.Error("保存成功后应显示「设置已保存」")
	}
}

func TestFlareSolverrCheckFailureShowsDetail(t *testing.T) {
	store := sscreen.NewMemStore(nsettings.Default())
	env := newEnv()
	solver := &servicestest.Solver{}
	solver.FailWith(errors.New("dial tcp 127.0.0.1:8191: connection refused"))
	r := openFlareSolverr(t, store, env, solver)
	r.scrollToEnd()

	r.clickNode(gfx.RoleButton, locales.WebCaptureSolverCheck())
	waitUntil(r, "错误提示", func() bool { return len(env.toasts(kit.ToastError)) > 0 })
	errs := env.toasts(kit.ToastError)
	if len(errs) != 1 || !strings.Contains(errs[0].Detail, "connection refused") {
		t.Errorf("应弹一条带原因的错误提示，得到 %+v", errs)
	}
	if r.hasText(locales.WebCaptureSolverConnected()) {
		t.Error("失败时不能显示「连接成功」")
	}
	waitUntil(r, "按钮恢复可用", func() bool {
		n, ok := r.node(gfx.RoleButton, locales.WebCaptureSolverCheck())
		return ok && !n.Disabled
	})
}

// 测试进行中两枚按钮禁用，防止重复触发；结束后恢复。
func TestFlareSolverrBusyDisablesButtons(t *testing.T) {
	store := sscreen.NewMemStore(nsettings.Default())
	env := newEnv()
	solver := &servicestest.Solver{Gate: make(chan struct{})}
	r := openFlareSolverr(t, store, env, solver)
	r.scrollToEnd()

	r.clickNode(gfx.RoleButton, locales.WebCaptureSolverCheck())
	for _, name := range []string{locales.WebCaptureSolverCheck(), locales.WebCaptureSolverSave()} {
		if n, ok := r.node(gfx.RoleButton, name); !ok || !n.Disabled {
			t.Errorf("测试进行中「%s」应禁用", name)
		}
	}
	close(solver.Gate)
	waitUntil(r, "测试结束", func() bool {
		n, ok := r.node(gfx.RoleButton, locales.WebCaptureSolverSave())
		return ok && !n.Disabled
	})
	if len(solver.Calls()) != 1 {
		t.Errorf("应只测试一次，得到 %d 次", len(solver.Calls()))
	}
}

// SSH 方式多出主机别名输入；保存连同连接方式一起写入。保存失败时不显示成功。
func TestFlareSolverrSSHAndSaveFailure(t *testing.T) {
	store := sscreen.NewMemStore(nsettings.Default())
	env := newEnv()
	r := openFlareSolverr(t, store, env, &servicestest.Solver{})
	r.scrollToEnd()

	if r.has(gfx.RoleTextField, locales.WebCaptureSolverHost()) {
		t.Fatal("本地方式不应出现 SSH 主机别名")
	}
	r.pick(locales.WebCaptureSolverConnection(), locales.WebCaptureSolverSsh())
	r.typeInto(locales.WebCaptureSolverHost(), "my-server")

	store.FailNext = errors.New("readonly database")
	r.clickNode(gfx.RoleButton, locales.WebCaptureSolverSave())
	if store.Values().App.FlareSolverr.Connection == "ssh" {
		t.Error("保存失败时设置不应改变")
	}
	if r.hasText(locales.WebCaptureSolverSaved()) {
		t.Error("保存失败时不能显示「设置已保存」")
	}
	if errs := env.toasts(kit.ToastError); len(errs) != 1 || !strings.Contains(errs[0].Detail, "readonly database") {
		t.Fatalf("应弹一条带原因的错误提示，得到 %+v", errs)
	}

	r.clickNode(gfx.RoleButton, locales.WebCaptureSolverSave())
	got := store.Values().App.FlareSolverr
	if got.Connection != "ssh" || got.SSHHost != "my-server" {
		t.Errorf("重试后应写入 SSH 配置，得到 %+v", got)
	}
}

// 外壳没接 FlareSolverr 服务时，测试连接如实报错而不是假装成功。
func TestFlareSolverrCheckWithoutService(t *testing.T) {
	env := newEnv()
	r := openFlareSolverr(t, sscreen.NewMemStore(nsettings.Default()), env, nil)
	r.scrollToEnd()
	r.clickNode(gfx.RoleButton, locales.WebCaptureSolverCheck())
	waitUntil(r, "错误提示", func() bool { return len(env.toasts(kit.ToastError)) > 0 })
	if r.hasText(locales.WebCaptureSolverConnected()) {
		t.Error("没有服务时不能显示「连接成功」")
	}
}

var _ services.FlareSolverr = (*servicestest.Solver)(nil)
