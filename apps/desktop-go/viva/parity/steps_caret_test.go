package parity

import (
	"image"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/tokens"
)

// caretSession 开一个只有一个聚焦文本框的无头会话（背景白、文字黑），返回会话与「数出画面里的深色像素」的函数。
func caretSession(t *testing.T) (*headless.Session, func() int) {
	t.Helper()
	look := kit.NewLook(kit.DefaultSpec())
	var field ui.Field
	white := gfx.RGBA(255, 255, 255, 1)
	page := ui.NewPage(ui.PageConfig{Background: &white, View: func(c ui.C) {
		look.BeginPaint()
		ui.Column(c).Width(300).Height(60).Pad(10).Children(func() {
			look.Input(c.Key("in"), &field, "x", "", kit.InputSpec{
				Size: 11, Weight: kit.WeightNormal, Color: tokens.RGBA(0, 0, 0, 1), Height: 17,
			}).Width(200).AutoFocus()
		})
	}})
	var canvas gfx.Canvas
	session, err := headless.Open(page.Config(app.Config{Title: t.Name(), Width: 300, Height: 60}), &canvas, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	dark := func() int {
		if err := session.Frame(); err != nil {
			t.Fatal(err)
		}
		img, err := session.Image()
		if err != nil {
			t.Fatal(err)
		}
		return countDark(img)
	}
	return session, dark
}

func countDark(img image.Image) int {
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r < 0x4000 && g < 0x4000 && bl < 0x4000 {
				n++
			}
		}
	}
	return n
}

// TestParkCaretDarkHidesTheCaretAtAnyElapsedTime：聚焦之后无论过了多久（亮相位、暗相位、再亮的相位里），
// ParkCaretDark 之后取图都看不到插入符；它本身落在暗相位中段，两侧都留有余量。
func TestParkCaretDarkHidesTheCaretAtAnyElapsedTime(t *testing.T) {
	for _, elapsed := range []time.Duration{0, 100, 400, 529, 530, 600, 795, 1059, 1060, 1200, 1590, 1700, 2100, 2650, 5000} {
		elapsed *= time.Millisecond
		session, dark := caretSession(t)
		since := session.Now()
		if got := dark(); got == 0 {
			t.Fatalf("聚焦那一帧应当画着插入符（实验前提），深色像素 %d", got)
		}
		if err := session.Advance(elapsed); err != nil {
			t.Fatal(err)
		}
		if err := ParkCaretDark(session, since); err != nil {
			t.Fatal(err)
		}
		if got := dark(); got != 0 {
			t.Errorf("聚焦后 %v 再 ParkCaretDark：画面里还有 %d 个深色像素（插入符没熄）", elapsed, got)
		}
		phase := (session.Now() - since) % (2 * components.CaretBlinkHalf)
		if phase < components.CaretBlinkHalf+components.CaretBlinkHalf/8 || phase > 2*components.CaretBlinkHalf-components.CaretBlinkHalf/8 {
			t.Errorf("聚焦后 %v：停在相位 %v，不在暗相位中段", elapsed, phase)
		}
	}
}

// TestParkCaretDarkBeforeAnyFocusedFrame：聚焦刚生效、还没有一帧带着焦点画出来就调用（场景步骤里按下 Ctrl+F 后紧接着调用的情形）。
// 插入符从第一帧带着焦点画出来起计时，ParkCaretDark 先出两帧再推进时钟；否则时钟一跳，计时从跳之后才起，取图时恰好亮着。
func TestParkCaretDarkBeforeAnyFocusedFrame(t *testing.T) {
	session, dark := caretSession(t)
	since := session.Now()
	if err := ParkCaretDark(session, since); err != nil {
		t.Fatal(err)
	}
	if got := dark(); got != 0 {
		t.Errorf("没出过任何带焦点的帧就 ParkCaretDark：画面里还有 %d 个深色像素（插入符没熄）", got)
	}
}

// TestParkCaretDarkLeavesTheClockWhenAlreadyDark：已经在暗相位中段就不再推进时钟（不平白消耗虚拟时间）。
func TestParkCaretDarkLeavesTheClockWhenAlreadyDark(t *testing.T) {
	session, _ := caretSession(t)
	since := session.Now()
	if err := session.Frame(); err != nil {
		t.Fatal(err)
	}
	if err := session.Advance(800 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	before := session.Now()
	if err := ParkCaretDark(session, since); err != nil {
		t.Fatal(err)
	}
	if after := session.Now(); after != before {
		t.Errorf("已在暗相位中段，时钟却从 %v 推进到 %v", before, after)
	}
}
