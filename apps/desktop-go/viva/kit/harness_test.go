package kit

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
)

// interfaceFont 是界面字体族（与 viva/app.InterfaceFont、parity 渲染一致）。
const interfaceFont = "Microsoft YaHei UI"

// still 是动效关闭的外观：所有过渡与入退场直接到终态，断言不受动画时钟影响。
func still() Spec {
	s := DefaultSpec()
	s.Motion = MotionLevelOff
	return s
}

// harness 是无头会话里的一页：不开真窗口、不碰 WebView2。
type harness struct {
	t       *testing.T
	look    *Look
	session *headless.Session
}

// openPage 用 spec 装配外观，在 width×height、缩放 1 的无头会话里打开 view，并出几帧到版面稳定。
// motion 为关闭档时让 Viva 的过渡直接到终态（与 viva/app 的 Root 同一语义）。
func openPage(t *testing.T, spec Spec, width, height int, view func(l *Look, c ui.C)) *harness {
	t.Helper()
	app.SetInterfaceFontFamily(interfaceFont)
	look := NewLook(spec)
	look.Scale = 1
	bg := look.Col(look.T.Background)
	page := ui.NewPage(ui.PageConfig{
		View: func(c ui.C) {
			c.Canvas().SetReduceMotion(spec.Motion == MotionLevelOff)
			look.BeginPaint()
			view(look, c)
		},
		Theme: &look.Theme, Background: &bg,
	})
	var canvas gfx.Canvas
	// OnKey 与窗口里一样接上 Tab 可达（reach.go）。
	config := page.Config(app.Config{Title: t.Name(), Width: width, Height: height, OnKey: look.OnKey})
	session, err := headless.Open(config, &canvas, headless.Options{Scale: 1})
	if err != nil {
		t.Fatalf("打开无头会话失败：%v", err)
	}
	t.Cleanup(session.Close)
	h := &harness{t: t, look: look, session: session}
	h.frames(4)
	return h
}

// frames 连续出帧（版面与转场要靠前一帧的结果，一帧不够）。
func (h *harness) frames(n int) {
	h.t.Helper()
	for i := 0; i < n; i++ {
		if err := h.session.Frame(); err != nil {
			h.t.Fatalf("出帧失败：%v", err)
		}
	}
}

// click 按标签点击控件，随后出几帧。
func (h *harness) click(label string) {
	h.t.Helper()
	if err := h.session.ClickLabel(label); err != nil {
		h.t.Fatalf("点击 %q 失败：%v", label, err)
	}
	h.frames(3)
}

// clickAt 点击页面坐标，随后出几帧。
func (h *harness) clickAt(x, y float32) {
	h.t.Helper()
	if err := h.session.Click(x, y); err != nil {
		h.t.Fatalf("点击 (%g,%g) 失败：%v", x, y, err)
	}
	h.frames(3)
}

// keys 发送按键（如 "Escape"、"Enter"、"Tab"），随后出几帧。
func (h *harness) keys(spec string) {
	h.t.Helper()
	if err := h.session.Keys(spec); err != nil {
		h.t.Fatalf("按键 %q 失败：%v", spec, err)
	}
	h.frames(3)
}

// node 取可访问节点（角色 + 名称）。
func (h *harness) node(role gfx.Role, name string) (app.AccessNode, bool) {
	return h.session.AccessByRole(role, name)
}

// has 报告角色 + 名称的节点是否存在。
func (h *harness) has(role gfx.Role, name string) bool {
	_, ok := h.node(role, name)
	return ok
}
