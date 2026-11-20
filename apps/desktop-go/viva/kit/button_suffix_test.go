package kit

import (
	"math"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/viva/tokens"
)

// 本文件守住按钮后缀的两个排法：SuffixPx（后缀自己的字号，如页签里 text-[10px] 的字数）与 SuffixFade（后缀取标签此刻的颜色再乘
// 不透明度，如 opacity-60 的 span）。两个都不给时沿用按钮字号档与 SuffixColor（批量按钮的计数）。

func suffixText(t *testing.T, h *harness, value string) gfx.Text {
	t.Helper()
	for _, tx := range h.session.Snapshot().Texts {
		if tx.Value == value {
			return tx
		}
	}
	t.Fatalf("没有找到后缀文字 %q", value)
	return gfx.Text{}
}

func TestButtonSuffixDefaultsToTheButtonTextSizeAndSuffixColor(t *testing.T) {
	spec := still()
	look := NewLook(spec)
	color := tokens.RGBA(220, 60, 40, 1)
	style := func(l *Look) ButtonStyle { return solidStyle(l, l.T.Background) }
	h := disabledPage(t, spec, ButtonSpec{Label: "添加", Suffix: "12", SuffixColor: color}, style)
	tx := suffixText(t, h, "12")
	if want := look.Rem(TextSM.Size); tx.Style.Size != want {
		t.Errorf("默认后缀字号应与按钮字号档相同 %.2f，实际 %.2f", want, tx.Style.Size)
	}
	if want := look.Col(color); tx.Style.Color != want {
		t.Errorf("默认后缀色应取 SuffixColor %+v，实际 %+v", want, tx.Style.Color)
	}
}

func TestButtonSuffixPxSetsTheSuffixFontSizeOnly(t *testing.T) {
	spec := still()
	look := NewLook(spec)
	style := func(l *Look) ButtonStyle { return solidStyle(l, l.T.Background) }
	h := disabledPage(t, spec, ButtonSpec{Label: "文字稿", Suffix: "10 字", SuffixPx: 10}, style)
	if got, want := suffixText(t, h, "10 字").Style.Size, float32(10); got != want {
		t.Errorf("SuffixPx 应按 %.0f 逻辑像素排后缀，实际 %.2f", want, got)
	}
	if got, want := suffixText(t, h, "文字稿").Style.Size, look.Rem(TextSM.Size); got != want {
		t.Errorf("标签仍按按钮字号档 %.2f，实际 %.2f", want, got)
	}
}

// SuffixFade 非零时后缀取标签此刻的颜色再乘它：不再用 SuffixColor；启用与禁用两态都跟着标签走。
func TestButtonSuffixFadeFollowsTheLabelColor(t *testing.T) {
	spec := still()
	style := func(l *Look) ButtonStyle { return solidStyle(l, l.T.Background) }
	for _, disabled := range []bool{false, true} {
		h := disabledPage(t, spec, ButtonSpec{
			Label: "文字稿", Suffix: "10 字", SuffixColor: tokens.RGBA(255, 0, 0, 1), SuffixFade: 0.6, Disabled: disabled,
		}, style)
		label := suffixText(t, h, "文字稿").Style.Color
		suffix := suffixText(t, h, "10 字").Style.Color
		if math.Abs(float64(suffix.Alpha-label.Alpha*0.6)) > 0.01 {
			t.Errorf("禁用=%v：后缀不透明度应是标签的 0.6 倍（%.3f），实际 %.3f", disabled, label.Alpha*0.6, suffix.Alpha)
		}
		if suffix.Red != label.Red || suffix.Green != label.Green || suffix.Blue != label.Blue {
			t.Errorf("禁用=%v：后缀应取标签的颜色通道 %+v，实际 %+v（不该是 SuffixColor 的红）", disabled, label, suffix)
		}
	}
}
