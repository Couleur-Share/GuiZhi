package kit

import "math"

// 文字基线对齐
//
// 同一行文字在渲染层（Chromium）与 Viva 里的基线位置不同，差零到两个设备像素，且随字号、字重、行高变化：
//
//   - Chromium（LayoutNG，设备缩放 1）：行盒顶到基线的距离 = floor((L − (A+D)) / 2) + A。
//     A、D 是该字体面的 ascent、descent 按字号取整（A = round(ascent·s)，D = round(descent·s)，四舍五入），
//     L 是行高；floor 来自 FontHeight::AddLeading 的 half_leading.Floor()。字体面取自 font-weight：
//     400 是 Microsoft YaHei UI，500 与 600 是 Microsoft YaHei UI Semibold，700 是 Microsoft YaHei UI Bold。
//     这个模型对 2026-11-16 在 React 参照里用隔离探针量出的 324 组（9 个字号 × 4 个字重 × 9 个行高，含小数字号与小数行高）
//     (内容区高度、内容区顶、基线) 全部吻合。
//   - Viva：界面字体的行不论字体，一律把基线放在纹理第 floor(H/2 + 0.378·s) 行（H 是纹理高，见
//     internal/textmetrics.LabelBaseline）；纹理的顶边按「四舍五入到整设备像素」落位（2026-11-16 在 0/0.25/0.5/0.75/1.0 的
//     小数顶边上实测：.25 向下取整，.5 与 .75 向上进位，见 frac_spike 的结论记录在 viva-ui-development.md §8）。
//
// 两者之差是一个整数设备像素。Text 用顶部内边距与底部负边距把文字整体平移这个差值，节点占的版面高度不变，
// 所以平移对周围布局无影响。行盒顶带小数（行高 17.5、24.5 这类）时，两边对顶边都是「四舍五入」，
// 整数平移不改变各自的进位，差值仍是同一个整数。守卫测试 baseline_test.go 用无头渲染核对 Viva 一侧的常数。
//
// 为什么必须是设备缩放 1 的参照：若 Chromium 实际按 200% 缩放排版（本机显示缩放 200% 且没有钉死设备缩放时就是如此），
// A、D、半行距都量化到 0.5 CSS 像素，同一字号的基线与上面的模型相差半像素。参照采集器已用
// --force-device-scale-factor=1 钉死，compare 会拒绝没有 forcedDsf 标记的陈旧参照。

// vivaBaselineBelowCenter 是 internal/textmetrics.BaselineBelowCenter（Viva v0.6.4），内部包不能导入，抄在这里。
const vivaBaselineBelowCenter = 0.378

// face 是一个字体面的垂直度量：OS/2 的 winAscent、winDescent，单位 em。
// 来源：Windows 11 自带的 msyh.ttc（YaHei UI，upem 2048）、msyhsb.ttc（Semibold，upem 1000）、msyhbd.ttc（Bold，upem 2048），
// 由 scripts/parity/tools/fontmetrics.mjs 读出；这三个字体里 hhea 度量与 win 度量相同。
type face struct{ ascent, descent float64 }

var (
	faceRegular  = face{2080.0 / 2048, 521.0 / 2048}
	faceSemibold = face{1056.0 / 1000, 271.0 / 1000}
	faceBold     = face{2080.0 / 2048, 521.0 / 2048}
)

// faceOfWeight 按 CSS 字重选字体面（Viva 字重刻度见 text.go）。
func faceOfWeight(weight float32) face {
	switch {
	case weight >= WeightBold-0.01:
		return faceBold
	case weight >= WeightMedium-0.01:
		return faceSemibold
	default:
		return faceRegular
	}
}

func roundHalfUp(v float64) float64 { return math.Floor(v + 0.5) }

// ChromeBaselineRow 是渲染层一行文字的基线到行盒顶的距离（设备像素，整数）。
func ChromeBaselineRow(weight float32, sizeDev, lineDev float64) int {
	f := faceOfWeight(weight)
	a := roundHalfUp(f.ascent * sizeDev)
	d := roundHalfUp(f.descent * sizeDev)
	return int(math.Floor((lineDev-(a+d))/2) + a)
}

// VivaLabelRows 是 Viva 给一行文字（盒高 line 逻辑像素）分配的纹理行数：painter 先把逻辑高度四舍五入到整数
// （app/painter.go appendTextLabel 的 roundPixel），平台再按缩放换成设备像素并向上取整。
// 行高带 0.25 小数（例如输入框的 字号+6 = 18.25）时，四舍五入与向上取整差一行；.5 与 .75 两者相同。
func VivaLabelRows(line, scale float32) int {
	if scale <= 0 {
		scale = 1
	}
	logical := math.Floor(float64(line) + 0.5)
	return int(math.Ceil(logical*float64(scale) - 1e-6))
}

// VivaBaselineRow 是 Viva 把一行文字的基线放在高 rows 行的纹理里的第几行。
func VivaBaselineRow(rows int, sizeDev float64) int {
	return int(math.Floor(float64(rows)/2 + vivaBaselineBelowCenter*sizeDev + 1e-6))
}

// ChromeInputBaselineY 是渲染层 <input> 里一行文字的基线落在哪一设备像素行（绝对坐标）。
//
// <input> 的内部编辑器在框内垂直居中，而且它的基线不像普通文字那样对半行距取整：
//
//	基线 = 四舍五入(框顶 + 框高/2 + (A − D)/2)，A、D 是取整的 ascent、descent（与 ChromeBaselineRow 同一套取整）。
//
// 它与行高、边框都无关（两项在居中里抵消）。普通文字是 round(行盒顶) + floor((L−(A+D))/2) + A，两者在行盒顶带小数时差一行：
// 2026-11-16 在 app.window.page-settings 的大字号档量到搜索框占位符比同基线的普通文字低 1 行。
// scripts/parity/tools/probe-input-text.mjs 在 React 参照里用 1040 组（字号 × 行高 × 框高 × 顶边小数）实测，
// testdata/chrome-input-probe.json 是夹具，TestChromeInputBaselineMatchesProbe 把本公式钉在它上面（字重 400）。
func ChromeInputBaselineY(weight float32, sizeDev, boxTopDev, boxHeightDev float64) int {
	f := faceOfWeight(weight)
	a := roundHalfUp(f.ascent * sizeDev)
	d := roundHalfUp(f.descent * sizeDev)
	return int(math.Floor(boxTopDev + boxHeightDev/2 + (a-d)/2 + 0.5))
}

// InputPad 返回让 Viva 输入框里那一行文字落在渲染层 <input> 文字位置所需的上、下内边距（逻辑像素）。
//
// Viva 的输入框（components.PaintInputContent）把高 size+6 的行盒居中在「内容区」里，基线 = round(行盒顶) + VivaBaselineRow。
// 令内容区恰好等于行盒（高 size+6），行盒顶 = 框顶 + top。取
//
//	top = 框高/2 + (A − D)/2 − VivaBaselineRow
//
// 则 Viva 的基线 = round(框顶 + top) + VivaBaselineRow = round(框顶 + 框高/2 + (A − D)/2)，与 ChromeInputBaselineY 相同，
// 而且与框顶的小数部分无关（top 带小数，整个框平移时两边同步进位）。bottom = 框高 − 行盒高 − top。
// 框矮到放不下行盒时返回的 bottom 会是负数，调用方应保证框高不小于 size+6。
func InputPad(weight, size, height, scale float32) (top, bottom float32) {
	if scale <= 0 {
		scale = 1
	}
	sizeDev := float64(size) * float64(scale)
	f := faceOfWeight(weight)
	a := roundHalfUp(f.ascent * sizeDev)
	d := roundHalfUp(f.descent * sizeDev)
	line := size + 6 // components.inputLineBox：fontSize+6
	rv := VivaBaselineRow(VivaLabelRows(line, scale), sizeDev)
	topDev := float64(height)*float64(scale)/2 + (a-d)/2 - float64(rv)
	top = float32(topDev / float64(scale))
	return top, height - line - top
}

// BaselineShift 返回把 Viva 的文字基线移到渲染层基线所需的平移，单位逻辑像素（设备像素差除以缩放）。
// 正值向下。size、line 是逻辑像素，scale 是设备像素与逻辑像素之比（缩放 100% 时为 1）。
func BaselineShift(weight float32, size, line, scale float32) float32 {
	if scale <= 0 {
		scale = 1
	}
	sizeDev := float64(size) * float64(scale)
	lineDev := float64(line) * float64(scale)
	rows := VivaLabelRows(line, scale)
	d := ChromeBaselineRow(weight, sizeDev, lineDev) - VivaBaselineRow(rows, sizeDev)
	return float32(d) / scale
}
