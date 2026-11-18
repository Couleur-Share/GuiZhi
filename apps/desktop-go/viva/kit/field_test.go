package kit

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// FieldInput 是渲染层 components/ui/Input.tsx（无 label、无 error）的对应：这里只验证行为与表面的状态切换，
// 像素对照在 viva/parity 的场景里。

type fieldPage struct {
	field     ui.Field
	spec      FieldSpec
	changed   int
	submitted int
}

func openFieldPage(t *testing.T, p *fieldPage) *harness {
	t.Helper()
	return openPage(t, still(), 400, 200, func(l *Look, c ui.C) {
		ui.Column(c.Key("page")).Fill().Pad(l.Space(6)).Gap(l.Space(4)).Children(func() {
			input := l.FieldInput(c.Key("field"), &p.field, p.spec)
			if input.Changed() {
				p.changed++
			}
			if input.Submitted() {
				p.submitted++
			}
			l.Button(c.Key("other"), ButtonSpec{Label: "别处"}, ButtonStyle{Height: l.Rem(2), PadX: l.Space(3)})
		})
	})
}

func TestFieldInputNamesItselfFromThePlaceholderUnlessGivenAName(t *testing.T) {
	h := openFieldPage(t, &fieldPage{spec: FieldSpec{Placeholder: "名称"}})
	if !h.has(gfx.RoleTextField, "名称") {
		t.Error("没有 Name 时，可访问名取自 Placeholder")
	}
	h = openFieldPage(t, &fieldPage{spec: FieldSpec{Name: "合集名称", Placeholder: "名称"}})
	if !h.has(gfx.RoleTextField, "合集名称") {
		t.Error("给了 Name 就以它为准")
	}
}

func TestFieldInputAutoFocusAsksOnceAndDoesNotPullTheFocusBack(t *testing.T) {
	p := &fieldPage{spec: FieldSpec{Placeholder: "名称", AutoFocus: true}}
	h := openFieldPage(t, p)
	if n, _ := h.node(gfx.RoleTextField, "名称"); !n.Focused {
		t.Fatal("AutoFocus 应在出现的那一帧取得焦点")
	}
	h.click("别处")
	if n, _ := h.node(gfx.RoleTextField, "名称"); n.Focused {
		t.Error("用户挪走焦点之后，AutoFocus 每帧都传也不应把焦点拉回来")
	}
}

func TestFieldInputReportsEditsAndEnterOnce(t *testing.T) {
	p := &fieldPage{spec: FieldSpec{Placeholder: "名称", AutoFocus: true}}
	h := openFieldPage(t, p)
	if err := h.session.Type("读书"); err != nil {
		t.Fatal(err)
	}
	h.frames(3)
	if p.field.Text() != "读书" || p.changed == 0 {
		t.Errorf("输入后 Text=%q changed=%d，期望「读书」且感知到编辑", p.field.Text(), p.changed)
	}
	if p.submitted != 0 {
		t.Error("没按回车不应提交")
	}
	h.keys("Enter")
	if p.submitted != 1 {
		t.Errorf("回车应恰好提交一次，实际 %d", p.submitted)
	}
}

// 聚焦时底色换成不透明的背景色、外面出一圈环；失焦后都收回（与边框一起按 duration-base 过渡，still 外观直接到终态）。
func TestFieldInputSurfaceFollowsFocus(t *testing.T) {
	p := &fieldPage{spec: FieldSpec{Placeholder: "名称"}}
	h := openFieldPage(t, p)
	n, _ := h.node(gfx.RoleTextField, "名称")
	inside := [2]float32{n.Bounds.X + 6, n.Bounds.Y + n.Bounds.Height/2} // 左内边距里，没有文字
	ring := [2]float32{n.Bounds.X + 30, n.Bounds.Y - 1}                  // 边框盒上沿外侧 1px：环占 2px
	page := pixelAt(h, 8, 8)                                             // 页面底色（输入框外面）

	if got := pixelAt(h, inside[0], inside[1]); got == page {
		t.Errorf("未聚焦时底色是半透明的 muted，不应等于页面底色 %v", page)
	}
	if got := pixelAt(h, ring[0], ring[1]); got != page {
		t.Errorf("未聚焦时没有环：外侧像素 %v 应等于页面底色 %v", got, page)
	}

	h.clickAt(n.Bounds.X+n.Bounds.Width/2, n.Bounds.Y+n.Bounds.Height/2)
	if got := pixelAt(h, inside[0], inside[1]); got != page {
		t.Errorf("聚焦后底色应是不透明的背景色 %v，实际 %v", page, got)
	}
	if got := pixelAt(h, ring[0], ring[1]); got == page {
		t.Errorf("聚焦后外侧应出一圈 primary/30 的环，实际仍是页面底色 %v", got)
	}

	h.click("别处")
	if got := pixelAt(h, ring[0], ring[1]); got != page {
		t.Errorf("失焦后环应收回，外侧像素 %v 应等于页面底色 %v", got, page)
	}
}
