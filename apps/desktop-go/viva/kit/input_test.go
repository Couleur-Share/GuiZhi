package kit

import (
	"encoding/json"
	"math"
	"os"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// TestChromeInputBaselineMatchesProbe 把 ChromeInputBaselineY 钉在 Chromium 的实测上：
// testdata/chrome-input-probe.json 是 scripts/parity/tools/probe-input-text.mjs --grid all 在设备缩放钉死为 1 的 React 参照里
// 量出的 1040 组（字号 × 行高 × 框高 × 顶边小数），每组记录「<input> 里文字的首墨行」比「同行盒顶普通文字的首墨行」低几行（delta）。
// 模型的预测差 = 输入框基线 − 普通文字基线（后者由已验证的 ChromeBaselineRow 给出），必须等于实测差。
func TestChromeInputBaselineMatchesProbe(t *testing.T) {
	raw, err := os.ReadFile("testdata/chrome-input-probe.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Res []struct {
			Size   float64 `json:"size"`
			Line   float64 `json:"line"`
			Height float64 `json:"height"`
			Top    float64 `json:"top"`
			Line0  float64 `json:"lineTop"`
			Delta  int     `json:"delta"`
		} `json:"res"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Res) < 1000 {
		t.Fatalf("夹具只有 %d 组，期望完整网格", len(doc.Res))
	}
	bad, nonzero := 0, 0
	for _, s := range doc.Res {
		ref := int(math.Floor(s.Line0+0.5)) + ChromeBaselineRow(WeightNormal, s.Size, s.Line)
		in := ChromeInputBaselineY(WeightNormal, s.Size, s.Top, s.Height)
		if s.Delta != 0 {
			nonzero++
		}
		if in-ref != s.Delta {
			bad++
			if bad <= 8 {
				t.Errorf("字号 %v 行高 %v 框高 %v 框顶 %v：模型预测差 %d，Chromium 实测差 %d", s.Size, s.Line, s.Height, s.Top, in-ref, s.Delta)
			}
		}
	}
	if bad > 0 {
		t.Errorf("共 %d/%d 组与模型不符", bad, len(doc.Res))
	}
	// 夹具必须真的含有「输入框比普通文字低一行」的情形，否则这个测试证明不了输入框与普通文字规则不同。
	if nonzero < 100 {
		t.Errorf("夹具里只有 %d 组差为 1，样本退化了", nonzero)
	}
}

// TestInputPadLandsOnChromeBaseline 用无头渲染核对：按 InputPad 的内边距摆的 Viva 输入框，
// 输入内容的基线落在 ChromeInputBaselineY 给的行上，并且与框顶的小数部分无关。
func TestInputPadLandsOnChromeBaseline(t *testing.T) {
	look := NewLook(DefaultSpec())
	black := tokens.RGBA(0, 0, 0, 1)
	type box struct{ size, height float32 }
	boxes := []box{{12.25, 31.5}, {14, 36}, {15.75, 40.5}, {15.75, 36}, {12.25, 28}, {14, 40}, {10.5, 28}}
	for _, b := range boxes {
		for _, frac := range []float32{0, 0.25, 0.5, 0.75} {
			top := 20 + frac
			var field ui.Field
			field.SetText("HHHH")
			page := ui.NewPage(ui.PageConfig{View: func(c ui.C) {
				ui.Column(c).Fill().PadTop(top).PadLeft(10).Background(gfx.RGBA(255, 255, 255, 1)).Children(func() {
					look.Input(c, &field, "测试", "", InputSpec{Size: b.size, Weight: WeightNormal, Color: black, Height: b.height}).Width(100)
				})
			}})
			var canvas gfx.Canvas
			session, err := headless.Open(page.Config(app.Config{Title: "input", Width: 140, Height: 100}), &canvas, headless.Options{Scale: 1})
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 3; i++ {
				if err := session.Frame(); err != nil {
					session.Close()
					t.Fatal(err)
				}
			}
			img, err := session.Image()
			session.Close()
			if err != nil {
				t.Fatal(err)
			}
			got := lastInkRow(img, 0, 0, 140, 100) + 1
			want := ChromeInputBaselineY(WeightNormal, float64(b.size), float64(top), float64(b.height))
			if got != want {
				t.Errorf("字号 %v 框高 %v 框顶 %v：Viva 基线行 %d，Chromium 模型 %d", b.size, b.height, top, got, want)
			}
		}
	}
}
