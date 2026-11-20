package kit

import (
	"encoding/json"
	"math"
	"os"
	"testing"

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
// 七种框 × 四种顶边小数共 28 个位置并排画进同一页：每个无头会话都要新建一次 D3D 设备（约 55 ms），逐个开会话
// 曾占掉整个包近四分之一的测试时间。格宽取整数，不改变各格顶边的小数部分。
func TestInputPadLandsOnChromeBaseline(t *testing.T) {
	look := NewLook(DefaultSpec())
	black := tokens.RGBA(0, 0, 0, 1)
	type box struct{ size, height float32 }
	boxes := []box{{12.25, 31.5}, {14, 36}, {15.75, 40.5}, {15.75, 36}, {12.25, 28}, {14, 40}, {10.5, 28}}
	fracs := []float32{0, 0.25, 0.5, 0.75}
	const cellInputW = 140
	fields := make([]ui.Field, len(boxes)*len(fracs))
	for i := range fields {
		fields[i].SetText("HHHH")
	}
	topOf := func(i int) float32 { return 20 + fracs[i%len(fracs)] }
	img := renderCells(t, len(fields), cellInputW, func(c ui.C, i int) {
		b := boxes[i/len(fracs)]
		ui.Column(c).Width(cellInputW).PadTop(topOf(i)).PadLeft(10).Children(func() {
			look.Input(c, &fields[i], "测试", "", InputSpec{Size: b.size, Weight: WeightNormal, Color: black, Height: b.height}).Width(100)
		})
	})
	for i := range fields {
		b, top := boxes[i/len(fracs)], topOf(i)
		got := lastInkRow(img, i*cellInputW, 0, cellInputW, cellH) + 1
		want := ChromeInputBaselineY(WeightNormal, float64(b.size), float64(top), float64(b.height))
		if got != want {
			t.Errorf("字号 %v 框高 %v 框顶 %v：Viva 基线行 %d，Chromium 模型 %d", b.size, b.height, top, got, want)
		}
	}
}

// TestInputTextareaLineMatchesChromium 核对 LineHeight/PadTop：文字落在渲染层 <textarea> 首行的位置（缩放 1）——
// 基线 = round(框顶 + PadTop) + ChromeBaselineRow(行高)，框顶的小数部分变化时也一样（这正是固定位移补不了的地方）。
func TestInputTextareaLineMatchesChromium(t *testing.T) {
	look := NewLook(DefaultSpec())
	black := tokens.RGBA(0, 0, 0, 1)
	// 三档字号下的标题框（1.375rem 半粗、leading-snug、pt-0.5）与几组不同的行高、上内边距，覆盖 <input> 居中与 textarea 首行差 0 与差 1 两种情形。
	type box struct {
		size, height, padTop, lineHeight float32
		weight                           float32
	}
	boxes := []box{
		{19.25, 28, 1.75, 26.46875, WeightSemibold},
		{22, 32, 2, 30.25, WeightSemibold},
		{24.75, 36, 2.25, 34.03125, WeightSemibold},
		{14, 36, 4, 21, WeightNormal},
		{15.75, 40.5, 3, 24.5, WeightNormal},
	}
	fracs := []float32{0, 0.25, 0.5, 0.75}
	const cellInputW = 140
	n := len(boxes) * len(fracs)
	fields := make([]ui.Field, n)
	for i := range fields {
		fields[i].SetText("HHHH")
	}
	at := func(i int) (b box, frac float32) { return boxes[i/len(fracs)], fracs[i%len(fracs)] }
	img := renderCells(t, n, cellInputW, func(c ui.C, i int) {
		b, frac := at(i)
		ui.Column(c).Width(cellInputW).PadTop(20 + frac).PadLeft(10).Children(func() {
			look.Input(c, &fields[i], "测试", "", InputSpec{
				Size: b.size, Weight: b.weight, Color: black, Height: b.height, PadTop: b.padTop, LineHeight: b.lineHeight,
			}).Width(100)
		})
	})
	for i := range fields {
		b, frac := at(i)
		got := lastInkRow(img, i*cellInputW, 0, cellInputW, cellH) + 1
		want := int(roundHalfUp(float64(20+frac+b.padTop))) + ChromeBaselineRow(b.weight, float64(b.size), float64(b.lineHeight))
		if got != want {
			t.Errorf("字号 %v 框高 %v 上内边距 %v 行高 %v 框顶小数 %v：基线行 %d，Chromium 模型 %d", b.size, b.height, b.padTop, b.lineHeight, frac, got, want)
		}
	}
}
