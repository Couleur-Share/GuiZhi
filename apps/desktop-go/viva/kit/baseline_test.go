package kit

import (
	"encoding/json"
	"image"
	"math"
	"os"
	"testing"

	"github.com/vivaui/viva/gfx"
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

// TestInlineBoxDrop 钉住「td（text-sm 支柱）里放 inline-flex 状态标签」时，标签比居中低多少。
// 期望值是 2026-11-19 在 app.window.library.table-columns 的 React 参照里量到的：标签外框顶对单元格居中位置的差
// （小档 行 [153.25 39.5]、标签 [166.375 15.5]，居中 165.25 → 1.125；中档 行 [174.5 45]、标签 [191 16] → 2；大档 行 [195.75 50.5]、标签 [215.5 16.5] → 2.75）。
// 标签：text-[10px] leading-none（10px 行高）、px-1.5 py-0.5、1px 边框，上内边距 = py + 1，盒高 = 10 + 2×上内边距，基线离顶 = 上内边距 + 8。
func TestInlineBoxDrop(t *testing.T) {
	cases := []struct {
		name              string
		strutSize, strutL float32
		pad               float32 // 标签的上内边距（py-0.5 + 1px 边框）
		want              float32
	}{
		{"小字号档 text-sm 12.25/17.5", 12.25, 17.5, 1.75 + 1, 1.125},
		{"中字号档 text-sm 14/20", 14, 20, 2 + 1, 2},
		{"大字号档 text-sm 15.75/22.5", 15.75, 22.5, 2.25 + 1, 2.75},
	}
	for _, c := range cases {
		base := float32(ChromeBaselineRow(WeightNormal, 10, 10)) // 8
		h, b := 10+2*c.pad, c.pad+base
		if got := InlineBoxDrop(WeightNormal, c.strutSize, c.strutL, h, b, 1); math.Abs(float64(got-c.want)) > 1e-6 {
			t.Errorf("%s：InlineBoxDrop = %v，期望 %v", c.name, got, c.want)
		}
	}
	// 行内盒与支柱是同一个行盒（高 20、基线离顶 15，就是 14px/20 常规字的行盒）：居中就是渲染层的位置，不能凭空多出偏移。
	if got := InlineBoxDrop(WeightNormal, 14, 20, 20, 15, 1); got != 0 {
		t.Errorf("与支柱重合的行内盒应没有偏移，得 %v", got)
	}
	// 缩放 2：同一个盒子按设备像素取整支柱的 ascent/descent，结果仍以逻辑像素给出（12.25px 的支柱在 2x 下 A=25、D=6，行高 35 设备像素）。
	if got := InlineBoxDrop(WeightNormal, 12.25, 17.5, 15.5, 10.75, 2); math.Abs(float64(got-1.125)) > 0.51 {
		t.Errorf("缩放 2 的偏移 %v 离缩放 1 的 1.125 超过半个逻辑像素", got)
	}
}

// TestVivaBaselineConstant 守住抄来的 Viva 常数：用无头渲染一行 "HHHH"，量出基线（H 竖笔的底边），
// 与 VivaBaselineRow 的公式比较。Viva 升级后这个常数变了，这里会先红。
// 六个字号并排画进同一页（每个无头会话都要新建一次 D3D 设备，逐个开会话是整个包最大的时间来源）；
// 每格的 "HHHH" 顶边对齐到 Y=10，基线 = 该格最后一行墨迹的下一行，相对文字盒顶。
func TestVivaBaselineConstant(t *testing.T) {
	type sample struct {
		size, height float32
	}
	samples := []sample{{12, 16}, {14, 20}, {18, 28}, {16, 24}, {20, 28}, {24, 32}}
	const top, width = 10, 120
	img := renderCells(t, len(samples), width, func(c ui.C, i int) {
		ui.Column(c).Width(width).Pad(top).Children(func() {
			ui.Text(c, "HHHH").FontSize(samples[i].size).LineHeight(samples[i].height).Color(gfx.RGBA(0, 0, 0, 1))
		})
	})
	for i, s := range samples {
		got := lastInkRow(img, i*width, top, width, int(s.height)+8) + 1 - top
		want := VivaBaselineRow(int(s.height), float64(s.size))
		if got != want {
			t.Errorf("字号 %v 行高 %v：渲染出的基线行 %d，公式 %d", s.size, s.height, got, want)
		}
	}
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
