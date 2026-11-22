package settings_test

import (
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/kit"
)

// sysEnv 是「网络」「快捷键」「关于」三页测试共用的环境：提示通道（不开窗口，用 Queued 核对弹了什么）、
// 日志记录器与不碰系统剪贴板的剪贴板。
type sysEnv struct {
	logs   *logRecorder
	notify *kit.Notifier
	copied []string
	// clipErr 非空时剪贴板写入失败（测复制失败的提示）。
	clipErr error
}

func newEnv() *sysEnv {
	e := &sysEnv{logs: &logRecorder{}, notify: kit.NewNotifier(nil)}
	e.notify.Clipboard = func(text string) error {
		if e.clipErr != nil {
			return e.clipErr
		}
		e.copied = append(e.copied, text)
		return nil
	}
	return e
}

func (e *sysEnv) guard() kit.Guard { return kit.Guard{Notify: e.notify, Log: e.logs} }

// toasts 返回某一类提示（按入队顺序）。
func (e *sysEnv) toasts(kind kit.ToastKind) []kit.QueuedToast {
	var out []kit.QueuedToast
	for _, q := range e.notify.Queued() {
		if q.Kind == kind {
			out = append(out, q)
		}
	}
	return out
}

// openPage 在 1280x800 的无头会话里打开 build 造出的页面（关闭动效，断言不受动画时钟影响）。
func openPage(t *testing.T, build func(look *kit.Look) func(c ui.C)) *rig {
	t.Helper()
	return openPageAt(t, 1280, build)
}

// openPageAt 同上，窗口宽度由调用方给（测响应式断点）。
func openPageAt(t *testing.T, width float32, build func(look *kit.Look) func(c ui.C)) *rig {
	t.Helper()
	fontOnce.Do(func() { app.SetInterfaceFontFamily("Microsoft YaHei UI") })
	spec := kit.DefaultSpec()
	spec.Motion = kit.MotionLevelOff
	look := kit.NewLook(spec)
	look.Scale = 1
	look.Viewport = width
	view := build(look)
	bg := look.Col(look.T.Background)
	page := ui.NewPage(ui.PageConfig{
		View: func(c ui.C) {
			c.Canvas().SetReduceMotion(true)
			view(c)
		},
		Theme: &look.Theme, Background: &bg,
	})
	var canvas gfx.Canvas
	session, err := headless.Open(page.Config(app.Config{Title: t.Name(), Width: int(width), Height: 800}), &canvas, headless.Options{Scale: 1})
	if err != nil {
		t.Fatalf("打开无头会话失败：%v", err)
	}
	t.Cleanup(session.Close)
	r := &rig{t: t, s: session}
	r.frames(4)
	return r
}

// waitUntil 逐帧等到条件成立（后台结果经邮箱回到界面线程，要出几帧才可见），最多约 2 秒。
func waitUntil(r *rig, what string, cond func() bool) {
	r.t.Helper()
	for i := 0; i < 400; i++ {
		r.frames(1)
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	r.t.Fatalf("等待超时：%s", what)
}

// hasText 报告画面上有没有含 sub 的文字（说明文字、状态行；不含控件的可访问名）。
func (r *rig) hasText(sub string) bool { return r.s.HasText(sub) }

// typeInto 点进文本框、全选后输入 text。
func (r *rig) typeInto(name, text string) {
	r.t.Helper()
	r.clickNode(gfx.RoleTextField, name)
	r.keys("cmd+a")
	if err := r.s.Type(text); err != nil {
		r.t.Fatalf("向「%s」输入 %q 失败：%v", name, text, err)
	}
	r.frames(3)
}

// fieldValue 取文本框此刻显示的值。
func (r *rig) fieldValue(name string) string {
	r.t.Helper()
	n, ok := r.node(gfx.RoleTextField, name)
	if !ok {
		r.t.Fatalf("找不到文本框「%s」", name)
	}
	return n.Value
}

// pick 在下拉里选一项：点开触发器，再点选项。
func (r *rig) pick(selectName, option string) {
	r.t.Helper()
	r.clickNode(gfx.RoleCombobox, selectName)
	r.clickNode(gfx.RoleOption, option)
}
