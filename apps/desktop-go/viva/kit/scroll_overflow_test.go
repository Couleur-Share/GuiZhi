package kit

import "testing"

// 溢出判定的边界：ScrollArea 的 0.5px 容差不含恰好 0.5px，ScrollAreaFlush 含（Chromium 按 1/64 像素判定，恰好 0.5px 的溢出也出滚动条槽）。
func TestOverflowsBoundary(t *testing.T) {
	tests := []struct {
		name      string
		content   float32
		inclusive bool
		want      bool
	}{
		{"没有溢出", 681, false, false},
		{"没有溢出（含边界）", 681, true, false},
		{"溢出 0.25px 在容差之内", 681.25, true, false},
		{"恰好 0.5px：ScrollArea 不算", 681.5, false, false},
		{"恰好 0.5px：ScrollAreaFlush 算", 681.5, true, true},
		{"超出 0.5px：两者都算", 682, false, true},
		{"超出 0.5px：两者都算（含边界）", 682, true, true},
	}
	for _, tc := range tests {
		if got := overflows(tc.content, 681, tc.inclusive); got != tc.want {
			t.Errorf("%s：overflows(%v, 681, %v) = %v，期望 %v", tc.name, tc.content, tc.inclusive, got, tc.want)
		}
	}
}
