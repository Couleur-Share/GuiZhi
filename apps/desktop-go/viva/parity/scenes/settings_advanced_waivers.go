package scenes

import (
	"slices"
	"unicode/utf8"

	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/parity"
)

// 设置三个外围分区（M3-2b：采集与转写、MCP、AI）的放过条款。只用已追认的类别（A-003、A-006、A-009、A-013、A-014、A-016、G-016），逐场景、带方向与区间；
// 登记范围的延伸写在台账对应条目的「M3-2b」段，待用户追认（见 C-010）。没有新增类别，没有改阈值。

// captureWaivers 是采集与转写页：一处字偶距、一枚虚线圆图标。
func captureWaivers() []parity.Waiver {
	return []parity.Waiver{
		{
			Ledger: "A-014", Layer: parity.LayerL2, Kind: "ink", Subject: "「SenseVoice 模型 · v1.2.0」", MaxDelta: 1,
			Note: "含拉丁字母的单行文字：Chromium 做字偶距、Viva 不做，墨迹水平 +1（宽度与外框一致）",
		},
		dashedCircleWaiver(" 12.00 12.00]", 0.55, 0.58, "0.55–0.57"),
		dashedCircleWaiver(" 10.50 10.50]", 0.60, 0.63, "0.61–0.62（小字号档，10.5px）"),
	}
}

// dashedCircleWaiver 登记 A-003 在虚线圆图标（中档 12px、小档 10.5px；大档读数落在原有阈值内）（lucide CircleDashed，8 段 1px 圆头短弧）上的延伸：「未安装（可选）」徽标里的 `h-3 w-3`。
// 几何与 lucide 0.460 的路径逐点一致，外框与位置一致；但每段短弧在 Viva 里画得比渲染层长约 1px、段间的缝被填住，墨迹总量偏多 0.55–0.57
// （浅色 0.574、深色 0.555）。这个量级比 A-003 其余登记（0.19–0.39）大得多：成因按「细笔画在两个栅格器里的覆盖率不同」归入 A-003，
// 但短圆头弧的差别没有逐项验证（见台账 A-003 的 M3-2b 段）。图标的主题只有外框，按边长加方向、区间框定（写法同 imageTabIconWaivers）。
func dashedCircleWaiver(size string, lo, hi float64, reading string) parity.Waiver {
	return parity.Waiver{
		Ledger: "A-003", Layer: parity.LayerL2, Kind: "ink-mass", Subject: size, Dir: 1, MinDelta: lo, MaxDelta: hi,
		Note: "虚线圆图标（CircleDashed）：短圆头弧的覆盖率与渲染层不同，墨迹总量偏多约 " + reading + "（外框、位置一致）",
	}
}

// statusValueWaivers 登记 A-013 在状态总览卡片的当前模型名上的延伸：卡片值是 `text-lg font-semibold tracking-tight`（-0.025em），
// Viva 的文字没有字距，每个字符都比渲染层宽 0.025em（text-lg 是 1.125rem，小、中、大字号档 15.75、18、20.25px，即 0.394、0.45、0.506px），
// 绘制宽度宽 字符数 × 字距，墨迹整体位移最多一半的宽度差。值独占卡片的一行，没有后续元素被带偏，不需要补盒宽（做法同 modalTitleWaivers）。
// 放不下的长名在两边都截成同一段前缀加「…」（报告里的主题：超过 20 个字符的取前 18 个加「…」），字符数按这个可见前缀算。
// 宽度差框在 [字符数×0.394−0.6, 字符数×0.506+0.6]（0.6 吸收 600 字重的字偶距，A-026 同类）。
func statusValueWaivers(values ...string) []parity.Waiver {
	var out []parity.Waiver
	seen := map[string]bool{}
	for _, v := range values {
		shown := v
		if r := []rune(v); len(r) > 20 {
			shown = string(r[:18]) + "…"
		}
		if seen[shown] {
			continue
		}
		seen[shown] = true
		n := float64(utf8.RuneCountInString(shown))
		subject := "「" + shown + "」"
		out = append(out,
			parity.Waiver{
				Ledger: "A-013", Layer: parity.LayerL2, Kind: "text-width", Subject: subject,
				MinDelta: max(n*0.394-0.6, 0.01), MaxDelta: n*0.506 + 0.6,
				Note: "tracking-tight 的字距：Viva 没有字距，状态卡片里当前模型名的绘制宽度宽 字符数 × 0.025em",
			},
			parity.Waiver{
				Ledger: "A-013", Layer: parity.LayerL2, Kind: "ink", Subject: subject, MaxDelta: n*0.506/2 + 1,
				Note: "同上：文字被拉宽，墨迹整体位移最多宽度差的一半（只管横向）",
			})
	}
	return out
}

// aiStatusValues 是夹具配置下状态总览六张卡片的值（路由解析到的模型显示名，没有的是「未配置」）；
// 夹具改了这里跟着改（对照的 T0 会因登记不再覆盖而报红）。
func aiStatusValues() []string {
	return []string{"gpt-4o (OpenAI)", "gpt-4o-mini (OpenAI)", "text-embedding-3-small (OpenAI)", locales.SettingsAiWorkbenchNotConfigured()}
}

// aiWaivers 是 AI 页（有数据的内容区）：状态卡片的当前模型名（字距）与 16px 的 TestTube 图标。
func aiWaivers() []parity.Waiver {
	return slices.Concat(statusValueWaivers(aiStatusValues()...), testTubeIconWaivers())
}

// testTubeIconWaivers 登记 A-003 在描边图标（「测试默认模型」钮的 TestTube）上的延伸：细笔画在 Skia 里偏淡，Viva 按精确面积覆盖，
// 墨迹总量偏多（外框、位置一致）。深色下偏得更多（浅色约 0.21–0.29、深色约 0.48–0.52：浅色笔画落在深色底上，两个栅格器的灰度校正差得更大），
// 所以分主题框区间。图标 1rem，三个字号档的边长是 14 / 16 / 18px，读数：小档浅 0.224 深 0.496、中档浅 0.24 深 0.52、大档浅 0.210 深 0.477。
// 图标的主题只有外框，按边长加方向、区间框定（写法同 imageTabIconWaivers）。
func testTubeIconWaivers() []parity.Waiver {
	var out []parity.Waiver
	for _, size := range []string{" 14.00 14.00]", " 16.00 16.00]", " 18.00 18.00]"} {
		out = append(out,
			parity.Waiver{
				Ledger: "A-003", Layer: parity.LayerL2, Kind: "ink-mass", Subject: size, Dir: 1, Variant: "light-", MinDelta: 0.20, MaxDelta: 0.30,
				Note: "描边图标（TestTube，浅色）：细笔画覆盖率与渲染层不同，墨迹总量偏多约 0.21–0.24（外框、位置一致）",
			},
			parity.Waiver{
				Ledger: "A-003", Layer: parity.LayerL2, Kind: "ink-mass", Subject: size, Dir: 1, Variant: "dark-", MinDelta: 0.45, MaxDelta: 0.58,
				Note: "描边图标（TestTube，深色）：同上，墨迹总量偏多约 0.48–0.52（外框、位置一致）",
			})
	}
	return out
}

// aiEmptyWaivers 是空配置场景：六张状态卡片都是「未配置」（字距，同 statusValueWaivers），
// 六个路由下拉全部 disabled:opacity-50，整组带透明度的文字在合成层里灰度抗锯齿（A-009）。
func aiEmptyWaivers() []parity.Waiver {
	return slices.Concat(statusValueWaivers(locales.SettingsAiWorkbenchNotConfigured()), testTubeIconWaivers(), []parity.Waiver{opacityInkWaiver("「跟随全局默认模型」")})
}

// guideMonoValues 是端点表单里地址说明卡的三段等宽值（夹具里的 OpenAI 预设与默认地址）：示例行、保存后的 Base URL、实际请求地址预览。
var guideMonoValues = []string{
	"https://api.openai.com · https://api.example.com/v1",
	"https://api.openai.com",
	"https://api.openai.com/v1/chat/completions",
}

// guideMonoWaivers 登记 A-006 在地址说明卡里的等宽值（`font-mono`，12px 档按 Consolas 每字符 0.5498em）上的延伸：Viva 把字格取整
// （中档 6.6→7px、小档 5.77→6px、大档约 7.42→8px），整行比渲染层宽 字符数 × 每字符 0.226–0.423px（读数：示例行 11.6–21.5px、
// Base URL 5.0–9.3px、请求地址 9.5–17.8px，三个字号档、深浅两个主题），墨迹水平位移累计到宽度差的约五分之一加 1px
// （读数 1–4px）。纵向不放过（基线已按 Consolas 度量对齐，台账 B-022）。宽度偏差按字符数与下限 字符数×0.2 框定，
// 与渲染层 Select 值、文本框里同样字样的非等宽文字（宽度差 0）区分开。
func guideMonoWaivers() []parity.Waiver {
	var out []parity.Waiver
	for _, v := range guideMonoValues {
		n := float64(utf8.RuneCountInString(v))
		subject := "「" + v + "」"
		out = append(out,
			parity.Waiver{
				Ledger: "A-006", Layer: parity.LayerL2, Kind: "text-width", Subject: subject, MinDelta: n * 0.2, MaxDelta: n*0.44 + 0.5,
				Note: "说明卡里的等宽值：Viva 把字格取整，整行比渲染层宽 字符数 × 0.23–0.42px",
			},
			parity.Waiver{
				Ledger: "A-006", Layer: parity.LayerL2, Kind: "ink", Subject: subject, MaxDelta: n*0.44/5 + 1,
				Note: "同上：字格取整使字形位置累计位移（横向，纵向不放过）",
			})
	}
	return out
}

// tinyGlyphWaiver 登记 G-016 在对话框里孤立小字形（单个「:」「.」）上的延伸：笔画只有几个像素，墨迹总量比随亚像素相位摆动，
// 比较器分不清「字形画错了」与「相位不同」。位置、宽度与 L3 照常比较。lo / hi 取该字形在 T2 全部变体里的读数（Viva 偏多）。
func tinyGlyphWaiver(glyph string, lo, hi float64, reading string) parity.Waiver {
	return parity.Waiver{
		Ledger: "G-016", Layer: parity.LayerL2, Kind: "ink-mass", Subject: "「" + glyph + "」", Dir: 1, MinDelta: lo, MaxDelta: hi,
		Note: "孤立的「" + glyph + "」：笔画只有几个像素，墨迹总量比随亚像素相位摆动（读数 " + reading + "）",
	}
}

// endpointDialogWaivers 是「添加供应商」对话框的专有登记：地址说明卡的标签冒号（读数 0.558–1.400）与三段等宽值。
func endpointDialogWaivers() []parity.Waiver {
	return slices.Concat(guideMonoWaivers(), []parity.Waiver{tinyGlyphWaiver(":", 0.5, 1.5, "0.558–1.400，T2 全部 36 个变体")})
}

// wizardWaivers 是「AI 快速配置」对话框的专有登记：进度条里「序号. 标签」的「.」（读数 0.511–2.265，深色偏多得更厉害）。
func wizardWaivers() []parity.Waiver {
	return []parity.Waiver{tinyGlyphWaiver(".", 0.5, 2.4, "0.511–2.265，T2 全部 36 个变体")}
}

// mcpWaivers 是 MCP 页：配置示例的 JSON 片段（font-mono 的 pre 里一整行）。读数（T1 1920×1080 中档、T2 小字号档，其余变体不出问题）：
// 小字号档墨迹总量偏少 0.261、1920 宽下 0.215（A-016：细笔画偏淡；只放方向为 Viva 偏少的 [0.1, 0.3]），1920 宽下墨迹水平错位 1–2px
// （A-006：字格取整，累计位移；纵向不放过）。位置、宽度与外框照常判定。
func mcpWaivers() []parity.Waiver {
	const snippet = "「{ \"mcpServers\""
	return []parity.Waiver{
		{
			Ledger: "A-016", Layer: parity.LayerL2, Kind: "ink-mass", Subject: snippet, Dir: -1, MinDelta: 0.1, MaxDelta: 0.3,
			Note: "配置示例 JSON 的等宽小字：拉丁细笔画在两个栅格器里覆盖率不同，Viva 的墨迹总量偏少 0.215–0.261",
		},
		{
			Ledger: "A-006", Layer: parity.LayerL2, Kind: "ink", Subject: snippet, MaxDelta: 2,
			Note: "同一行等宽文字：字格取整使字形位置累计差 1–2 像素（只管横向）",
		},
	}
}
