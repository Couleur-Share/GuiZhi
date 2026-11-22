package kit

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/ui"
)

// 占位符折成多行时，行距默认是 Viva 多行框的行高（字号加 7）；给了 PlaceholderLeading 就按它走（渲染层 leading-5 的 1.25rem），
// 首行位置不变，只有第二行起的间距变。
func TestTextAreaPlaceholderLeading(t *testing.T) {
	const placeholder = "alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu"
	pitch := func(leading float32) float32 {
		var f ui.Field
		h := openPage(t, still(), 220, 200, func(l *Look, c ui.C) {
			ui.Column(c.Key("page")).Width(200).Children(func() {
				l.TextArea(c.Key("ta"), &f, "label", placeholder, TextAreaSpec{
					Height: 160, PadX: 8, PadY: 8, Color: l.T.Foreground, Placeholder: l.T.MutedForeground, PlaceholderLeading: leading,
				}).FillWidth()
			})
		})
		var ys []float32
		for _, text := range h.session.Snapshot().Texts {
			if text.Value != "" && strings.Contains(placeholder, strings.TrimSpace(text.Value)) {
				ys = append(ys, text.Bounds.Y)
			}
		}
		if len(ys) < 2 {
			t.Fatalf("占位符应折成至少两行，实际只画出 %d 段文字：%q", len(ys), h.session.Texts())
		}
		return ys[1] - ys[0]
	}
	def := pitch(0)
	custom := pitch(def - 1)
	if custom != def-1 {
		t.Fatalf("PlaceholderLeading 给了 %v，占位符的行距却是 %v", def-1, custom)
	}
}
