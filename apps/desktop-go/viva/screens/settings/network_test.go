package settings_test

import (
	"errors"
	"fmt"
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

// openNetwork 打开「网络」页；solver 为 nil 表示外壳没接 FlareSolverr 服务。
func openNetwork(t *testing.T, store sscreen.Store, env *sysEnv, solver *servicestest.Solver) *rig {
	t.Helper()
	return openPage(t, func(look *kit.Look) func(c ui.C) {
		n := &sscreen.Network{Look: look, Store: store, Guard: env.guard(), Wake: func() {}}
		if solver != nil {
			n.Solver = solver
		}
		t.Cleanup(n.Close)
		return n.Page
	})
}

func manualProxyStore() *sscreen.MemStore {
	v := nsettings.Default()
	v.App.NetworkProxy = nsettings.NetworkProxy{Mode: "manual", Protocol: "http", Host: "127.0.0.1", Port: 7890}
	return sscreen.NewMemStore(v)
}

// 手动代理：模式、协议、主机、端口、用户名、口令、绕过列表逐项写入设置；口令框默认是圆点，点眼睛才显示明文。
func TestNetworkManualProxyPersists(t *testing.T) {
	store := sscreen.NewMemStore(nsettings.Default())
	env := newEnv()
	r := openNetwork(t, store, env, nil)

	if r.has(gfx.RoleTextField, locales.SettingsNetworkProxyHost()) {
		t.Fatal("默认模式下不应出现手动代理的输入框")
	}
	r.pick(locales.SettingsNetworkProxyMode(), locales.SettingsNetworkProxyModeManual())
	if got := store.Values().App.NetworkProxy.Mode; got != "manual" {
		t.Fatalf("选「手动代理」后模式 = %q", got)
	}
	r.pick(locales.SettingsNetworkProxyProtocol(), "SOCKS5")
	r.typeInto(locales.SettingsNetworkProxyHost(), "proxy.local")
	r.typeInto(locales.SettingsNetworkProxyPort(), "1080")
	r.typeInto(locales.SettingsNetworkProxyUsername(), "alice")
	r.typeInto(locales.SettingsNetworkProxyPassword(), "hunter2")
	r.typeInto(locales.SettingsNetworkProxyBypass(), "localhost,10.0.0.1")

	p := store.Values().App.NetworkProxy
	if p.Protocol != "socks5" || p.Host != "proxy.local" || p.Port != 1080 || p.Username != "alice" || p.Password != "hunter2" || p.Bypass != "localhost,10.0.0.1" {
		t.Errorf("写入的代理设置不对：%s（口令长度 %d）", p.String(), len(p.Password))
	}
	if errs := env.toasts(kit.ToastError); len(errs) != 0 {
		t.Errorf("正常编辑不应弹错误：%v", errs)
	}

	// 口令框：默认遮蔽，可访问值里没有明文；点「显示密码」后才是明文。
	n, _ := r.node(gfx.RoleTextField, locales.SettingsNetworkProxyPassword())
	if !n.Secure || strings.Contains(n.Value, "hunter2") {
		t.Errorf("口令框默认应遮蔽：Secure=%v Value=%q", n.Secure, n.Value)
	}
	r.clickNode(gfx.RoleButton, locales.CommonShowPassword())
	if n, _ = r.node(gfx.RoleTextField, locales.SettingsNetworkProxyPassword()); n.Secure {
		t.Error("点「显示密码」后口令应显示明文")
	}
	if !r.has(gfx.RoleButton, locales.CommonHidePassword()) {
		t.Error("显示明文后按钮应变成「隐藏密码」")
	}
}

// 写入失败：设置不变、文本框退回原值，错误提示带原因；口令不会出现在提示或日志里。
func TestNetworkProxyWriteFailureRevertsAndExplains(t *testing.T) {
	v := nsettings.Default()
	v.App.NetworkProxy = nsettings.NetworkProxy{Mode: "manual", Protocol: "http", Host: "127.0.0.1", Port: 7890, Username: "alice", Password: "hunter2"}
	store := sscreen.NewMemStore(v)
	env := newEnv()
	r := openNetwork(t, store, env, nil)

	store.FailNext = errors.New("disk full")
	r.typeInto(locales.SettingsNetworkProxyHost(), "other.host")
	if got := store.Values().App.NetworkProxy.Host; got != "127.0.0.1" || store.Updates != 0 {
		t.Errorf("写入失败时设置不应改变：Host=%q 写入 %d 次", got, store.Updates)
	}
	if got := r.fieldValue(locales.SettingsNetworkProxyHost()); got != "127.0.0.1" {
		t.Errorf("写入失败后文本框应退回原值，得到 %q", got)
	}
	errs := env.toasts(kit.ToastError)
	if len(errs) != 1 || !strings.Contains(errs[0].Detail, "disk full") {
		t.Fatalf("应弹一条带原因的错误提示，得到 %+v", errs)
	}
	if all := fmt.Sprint(env.notify.Queued(), env.logs.all()); strings.Contains(all, "hunter2") {
		t.Error("提示与日志里不能出现代理口令")
	}
	if got := env.logs.all(); len(got) != 1 || !strings.Contains(got[0], "disk full") {
		t.Errorf("失败应记一条带原因的日志，得到 %q", got)
	}

	// 用户再改一次就是重试，成功后写入。
	r.typeInto(locales.SettingsNetworkProxyHost(), "retry.host")
	if got := store.Values().App.NetworkProxy.Host; got != "retry.host" {
		t.Errorf("重试后应写入，Host=%q", got)
	}
}

// 更新镜像开关写入 UseUpdateMirror；写入失败时开关保持原状并提示。
func TestNetworkMirrorToggle(t *testing.T) {
	store := sscreen.NewMemStore(nsettings.Default())
	env := newEnv()
	r := openNetwork(t, store, env, nil)
	name := locales.SettingsTryMirrorSource()

	store.FailNext = errors.New("locked")
	r.clickNode(gfx.RoleSwitch, name)
	if store.Values().UI.UseUpdateMirror || r.switchChecked(name) {
		t.Error("写入失败时开关应保持关")
	}
	if errs := env.toasts(kit.ToastError); len(errs) != 1 || !strings.Contains(errs[0].Detail, "locked") {
		t.Errorf("应弹一条带原因的错误提示，得到 %+v", errs)
	}
	r.clickNode(gfx.RoleSwitch, name)
	if !store.Values().UI.UseUpdateMirror || !r.switchChecked(name) {
		t.Error("重试成功后开关应打开并写入设置")
	}
}

// ── FlareSolverr 块（临时挂在网络页最下面）──

func TestFlareSolverrSaveAndTestUseDraft(t *testing.T) {
	store := sscreen.NewMemStore(nsettings.Default())
	env := newEnv()
	solver := &servicestest.Solver{}
	r := openNetwork(t, store, env, solver)
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
	r := openNetwork(t, store, env, solver)
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
	r := openNetwork(t, store, env, solver)
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
	r := openNetwork(t, store, env, &servicestest.Solver{})
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
	r := openNetwork(t, sscreen.NewMemStore(nsettings.Default()), env, nil)
	r.scrollToEnd()
	r.clickNode(gfx.RoleButton, locales.WebCaptureSolverCheck())
	waitUntil(r, "错误提示", func() bool { return len(env.toasts(kit.ToastError)) > 0 })
	if r.hasText(locales.WebCaptureSolverConnected()) {
		t.Error("没有服务时不能显示「连接成功」")
	}
}

var _ services.FlareSolverr = (*servicestest.Solver)(nil)
