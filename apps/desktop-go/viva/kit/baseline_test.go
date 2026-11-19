package kit

import (
	"encoding/json"
	"image"
	"os"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
)

// TestChromeBaselineMatchesProbe 把 ChromeBaselineRow 钉在 Chromium 的实测上：testdata/chrome-text-probe.json 是
// scripts/parity/tools/probe-text-metrics.mjs 在设备缩放钉死为 1 的 React 参照里量出的样本（含小数字号与小数行高），
// 每一组的「基线到行盒顶」必须等于模型值。Chromium 或字体变了，重新生成夹具后这里会先红。
func TestChromeBaselineMatchesProbe(t *testing.T) {
	raw, err := os.ReadFile("testdata/chrome-text-probe.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Samples []struct {
			Size, Line, Baseline float64
			Weight               int
		} `json:"samples"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Samples) < 300 {
		t.Fatalf("夹具样本只有 %d 组，期望覆盖完整矩阵", len(doc.Samples))
	}
	bad := 0
	for _, s := range doc.Samples {
		if got := ChromeBaselineRow(vivaWeightOfCSS(t, s.Weight), s.Size, s.Line); float64(got) != s.Baseline {
			bad++
			if bad <= 8 {
				t.Errorf("字号 %v 字重 %v 行高 %v：模型基线 %d，Chromium %v", s.Size, s.Weight, s.Line, got, s.Baseline)
			}
		}
	}
	if bad > 0 {
		t.Errorf("共 %d/%d 组样本与模型不符", bad, len(doc.Samples))
	}
}

// TestChromeContentRowsMatchProbe 把 ChromeContentRows（基线之上 A 行、之下 D 行）也钉在同一份实测上：
// 夹具里的 contentTop 与 contentHeight 是渲染层 Range 取到的文字外框（相对行盒顶），即 A = 基线 − contentTop、
// A + D = contentHeight。G-011 的「字形是否被裁剪线切掉」按这两个数判，两边用同一把尺子，所以 A 与 D 各自都得和 Chromium 一致
// （只核对 A + D 会放过「A 多一行、D 少一行」的互补错误）。
func TestChromeContentRowsMatchProbe(t *testing.T) {
	raw, err := os.ReadFile("testdata/chrome-text-probe.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Samples []struct {
			Size, Baseline, ContentTop, ContentHeight float64
			Weight                                    int
		} `json:"samples"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Samples) < 300 {
		t.Fatalf("夹具样本只有 %d 组，期望覆盖完整矩阵", len(doc.Samples))
	}
	// 字号 × 字重去重：A、D 只与它们有关（行高不影响内容区的大小）。
	type key struct {
		size   float64
		weight int
	}
	seen := map[key]bool{}
	bad := 0
	for _, s := range doc.Samples {
		if s.ContentHeight == 0 {
			t.Fatalf("夹具缺 contentHeight：字号 %v 字重 %v（需要重新生成）", s.Size, s.Weight)
		}
		k := key{s.Size, s.Weight}
		if seen[k] {
			continue
		}
		seen[k] = true
		a, d := ChromeContentRows(vivaWeightOfCSS(t, s.Weight), s.Size)
		wantA := s.Baseline - s.ContentTop
		if float64(a) != wantA || float64(a+d) != s.ContentHeight {
			bad++
			if bad <= 8 {
				t.Errorf("字号 %v 字重 %v：模型 A=%d D=%d（高 %d），Chromium A=%v D=%v（高 %v）",
					s.Size, s.Weight, a, d, a+d, wantA, s.ContentHeight-wantA, s.ContentHeight)
			}
		}
	}
	if bad > 0 {
		t.Errorf("共 %d/%d 组（字号×字重）与模型不符", bad, len(seen))
	}
	if len(seen) < 20 {
		t.Errorf("字号×字重只有 %d 组，样本太少", len(seen))
	}
}

// vivaWeightOfCSS 把夹具里的 CSS 字重换成 Viva 的字重刻度（theme.go：Regular 0、Medium 0.15、Semibold 0.23、Bold 0.40）。
func vivaWeightOfCSS(t *testing.T, css int) float32 {
	t.Helper()
	switch css {
	case 400:
		return WeightNormal
	case 500:
		return WeightMedium
	case 600:
		return WeightSemibold
	case 700:
		return WeightBold
	}
	t.Fatalf("夹具里有未映射的 CSS 字重 %d", css)
	return 0
}

// TestBaselineShiftKnownCases 钉住几处代表性案例的平移量（Microsoft YaHei UI，设备像素 1:1）。
// 期望值由模型两端的公式手算，并由 settings.general 的 React/Viva 墨迹对照在 2026-11-16 复核（设备缩放钉死为 1 的参照）。
func TestBaselineShiftKnownCases(t *testing.T) {
	cases := []struct {
		name         string
		weight       float32
		size, line   float32
		wantShiftDev float32
	}{
		{"h1 18px/600/行高 28", WeightSemibold, 18, 28, 1},
		{"label 14px/500/行高 20", WeightMedium, 14, 20, 0},
		{"描述 12px/400/行高 16", WeightNormal, 12, 16, 0},
		{"select 值 14px/400/行高 20", WeightNormal, 14, 20, 0},
		{"小字号 label 12.25px/500/行高 17.5（根字号 14px）", WeightMedium, 12.25, 17.5, 0},
		{"小字号描述 10.5px/400/行高 14", WeightNormal, 10.5, 14, 1},
		{"小字号 h1 15.75px/600/行高 24.5", WeightSemibold, 15.75, 24.5, 0},
	}
	for _, c := range cases {
		if got := BaselineShift(c.weight, c.size, c.line, 1); got != c.wantShiftDev {
			t.Errorf("%s：平移 %v，期望 %v", c.name, got, c.wantShiftDev)
		}
	}
}

// TestStrutTextTop 钉住「块容器里直接放一个行内 span」时，行盒顶到 span 行盒顶的距离（text-xs：0.75rem / 1rem）。
// 期望值由 LayoutNG 的行内盒模型手算（A、D 各自取整，半行距向下取整，行盒上沿取支柱与 span 中较高的）；
// 小字号档的 4px 与 2026-11-18 在 app.window.library.dlg-collection-new 量到的「图标」标签位置相符：
// 标签内容区顶 292.5 = 名称输入框底 274.5 + 间距 14 + 4。
func TestStrutTextTop(t *testing.T) {
	cases := []struct {
		name             string
		root, span, line float64
		weight           float32
		want             float64
	}{
		{"中档 12px/16 常规", 16, 12, 16, WeightNormal, 6},
		{"中档 12px/16 半粗：A=13，比常规的 12 高一行", 16, 12, 16, WeightSemibold, 5},
		{"小档 10.5px/14 常规", 14, 10.5, 14, WeightNormal, 4},
		{"大档 13.5px/18 常规", 18, 13.5, 18, WeightNormal, 6},
		{"span 比支柱高时没有空白", 14, 28, 36, WeightNormal, 0},
	}
	for _, c := range cases {
		if got := StrutTextTop(c.root, c.span, c.line, c.weight); got != c.want {
			t.Errorf("%s：StrutTextTop = %v，期望 %v", c.name, got, c.want)
		}
	}
}

// TestVivaBaselineConstant 守住抄来的 Viva 常数：用无头渲染一行 "HHHH"，量出基线（H 竖笔的底边），
// 与 VivaBaselineRow 的公式比较。Viva 升级后这个常数变了，这里会先红。
func TestVivaBaselineConstant(t *testing.T) {
	type sample struct {
		size, height float32
	}
	for _, s := range []sample{{12, 16}, {14, 20}, {18, 28}, {16, 24}, {20, 28}, {24, 32}} {
		got := renderedBaselineRow(t, s.size, s.height)
		want := VivaBaselineRow(int(s.height), float64(s.size))
		if got != want {
			t.Errorf("字号 %v 行高 %v：渲染出的基线行 %d，公式 %d", s.size, s.height, got, want)
		}
	}
}

// renderedBaselineRow 画一行黑色 "HHHH"（顶边对齐到 Y=10），返回基线相对文字盒顶的行数。
func renderedBaselineRow(t *testing.T, size, height float32) int {
	t.Helper()
	const top = 10
	page := ui.NewPage(ui.PageConfig{View: func(c ui.C) {
		ui.Column(c).Pad(top).Background(gfx.RGBA(255, 255, 255, 1)).Children(func() {
			ui.Text(c, "HHHH").FontSize(size).LineHeight(height).Color(gfx.RGBA(0, 0, 0, 1))
		})
	}})
	var canvas gfx.Canvas
	session, err := headless.Open(page.Config(app.Config{Title: "baseline", Width: 120, Height: 80}), &canvas, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for i := 0; i < 3; i++ {
		if err := session.Frame(); err != nil {
			t.Fatal(err)
		}
	}
	img, err := session.Image()
	if err != nil {
		t.Fatal(err)
	}
	return lastInkRow(img, 0, top, 120, int(height)+8) + 1 - top
}

// lastInkRow 返回区域内最后一行含“明显深色”像素的行号（绝对行号）。
func lastInkRow(img image.Image, x, y, w, h int) int {
	last := -1
	for yy := y; yy < y+h; yy++ {
		for xx := x; xx < x+w; xx++ {
			r, g, b, _ := img.At(xx, yy).RGBA()
			if (r+g+b)/3 < 0x8000 {
				last = yy
				break
			}
		}
	}
	return last
}
