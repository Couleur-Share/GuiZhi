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

// ChromeContentRows 是渲染层一行文字的「内容区」在基线之上、之下各占的行数（设备像素，整数）：A、D 按字号取整，
// 与 ChromeBaselineRow 同一套取整。渲染层用 Range 取到的文字外框就是内容区（顶 = 基线 − A，高 = A + D）；
// 对照采集拿它判断「裁剪线是不是切到了字形所在的位置」，两边用同一把尺子（台账 G-011）。
func ChromeContentRows(weight float32, sizeDev float64) (ascent, descent int) {
	f := faceOfWeight(weight)
	return int(roundHalfUp(f.ascent * sizeDev)), int(roundHalfUp(f.descent * sizeDev))
}

// StrutTextTop 是渲染层「块容器里直接放一个行内 span」（<div><span className="text-xs …">）时，行盒顶到 span 行盒顶的距离。
//
// span 落在匿名行盒里，与容器继承来的正文行（支柱 strut：根字号、行高 1.5、常规字重）按基线对齐，行盒上沿由两者中较高的撑起。
// 每个行内盒「基线之上」的高度是 A + floor((L − (A+D))/2)（就是 ChromeBaselineRow），所以
//
//	距离 = max(支柱基线之上, span 基线之上) − span 基线之上。
//
// 根字号 16px、12px 的 span（行高 16px）时是 6px（0.375rem）；但 A、D 与半行距各自取整，不能按 rem 缩放：
// 14px 根字号（10.5px 的 span）是 4px 而不是 5.25px，18px 根字号（13.5px 的 span）是 6px 而不是 6.75px
// （2026-11-18，知识库弹窗的「图标」标签在小字号档比渲染层低一行，台账 M2-1 对照记录）。
func StrutTextTop(rootPx, spanPx, spanLinePx float64, spanWeight float32) float64 {
	strut := float64(ChromeBaselineRow(WeightNormal, rootPx, rootPx*1.5))
	span := float64(ChromeBaselineRow(spanWeight, spanPx, spanLinePx))
	return math.Max(strut, span) - span
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

// InlineFlexExtra 是「在一行文字的行盒里放一个行内弹性盒（图标加文字，items-center）」比一行文字多出的高度，单位逻辑像素。
//
// 渲染层里 <span class="inline-flex items-center"> 的基线取第一项（图标）的下沿：图标没有基线，由它的边框盒下沿合成，
// 而图标在弹性盒里居中，下沿离盒顶 B = (H − icon)/2 + icon（H 是弹性盒高，取文字行高与图标的较大者）。
// 行盒按基线对齐，父行的文字基线离行盒顶 sb（ChromeBaselineRow）：B 比 sb 大，弹性盒顶部就探出行盒顶 B − sb，
// 行盒整体向上长出这一截（内容落在行盒的上部，下面留空）；B 不比 sb 大时不长。
//
// size、line 是父行文字的字号与行高，icon 是图标边长，都是逻辑像素；weight 与 scale 同 BaselineShift。
// 2026-11-17 在中号字（根字号 16）的任务详情弹窗里与渲染层实测吻合：12px 图标（提交来源）多 2px、14px 图标（副标题）多 3px。
func InlineFlexExtra(weight float32, size, line, icon, scale float32) float32 {
	if scale <= 0 {
		scale = 1
	}
	h := max(line, icon)
	b := (h-icon)/2 + icon
	sb := float32(ChromeBaselineRow(weight, float64(size)*float64(scale), float64(line)*float64(scale))) / scale
	return max(0, b-sb)
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
