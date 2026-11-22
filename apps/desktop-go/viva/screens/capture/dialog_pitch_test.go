package capture

import "testing"

// 钉住一处已知、未修的平台差异（M3-3 Step 2 对拍暴露，台账 B-062 同一成因）：Viva 多行输入框（ui.TextArea）每一行的行高写死为
// 字号加 7（components/textarea.go：lineHeight := fontSize + 7），14px 字号是 21px；渲染层 text-sm 的行高是 20px。
// 草稿框里的文字一旦折成两行以上（回车换行或超过一行宽），第 2 行起每行比渲染层低 1 像素（对拍的 L2 `wrapped-pitch` 与 `ink` 各一处）。
// 四个对照场景（capture.dlg.*）的草稿都放得进一行，所以它们没有触发这个差异，也不能据此说多行草稿与渲染层一致。
//
// 本测试守的是「差异仍然存在、大小就是 1px」：哪天 Viva 允许设置多行框的行高（或 kit.TextArea 自建了行盒），这里会失败，
// 提醒把说明与台账一起更新，并补上多行草稿的对照场景。它不是在宣称 21px 是对的。
func TestDraftRowPitchIsVivasTextAreaLeadingNotTheRenderers(t *testing.T) {
	r := opened(t)
	r.draftWith("第一行")
	r.keys("enter")
	r.typeText("第二行")

	var first, second float32 = -1, -1
	for _, text := range r.session.Snapshot().Texts {
		switch text.Value {
		case "第一行":
			first = text.Bounds.Y
		case "第二行":
			second = text.Bounds.Y
		}
	}
	if first < 0 || second < 0 {
		t.Fatalf("画面上找不到草稿的两行文字：%q", r.session.Texts())
	}
	// 14px 字号：Viva 行高 21px；渲染层是 20px。
	const vivaLeading, rendererLeading = 21, 20
	if got := second - first; got != vivaLeading {
		t.Fatalf("草稿两行的行距 = %v px，登记的是 %v px（渲染层是 %v px，差 1px 是已知差异）：差异变了，请更新说明与台账", got, float32(vivaLeading), float32(rendererLeading))
	}
}
