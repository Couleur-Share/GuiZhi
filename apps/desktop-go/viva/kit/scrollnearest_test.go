package kit

import "testing"

// TestScrollNearest 逐条核对 scrollIntoView 的 nearest 取值（视口 [0, 100]）。
func TestScrollNearest(t *testing.T) {
	cases := []struct {
		name string
		a, b float32
		want float32
	}{
		{"完全在视口内", 10, 50, 0},
		{"贴着两边", 0, 100, 0},
		{"比视口窄、起边在视口之外（补回上边）", -20, 10, -20},
		{"比视口窄、终边在视口之外（补回下边）", 80, 130, 30},
		{"比视口宽、起边在视口之内、终边在外（对齐起边：表格行的横向）", 17, 1033, 17},
		{"比视口宽、起边在视口之外、终边在内（对齐终边）", -300, 90, -10},
		{"元素把视口整个盖住：不动", -50, 150, 0},
		{"整个在视口之外的下方（对齐终边）", 120, 160, 60},
		{"整个在视口之外的上方（对齐起边）", -60, -20, -60},
	}
	for _, c := range cases {
		if got := ScrollNearest(c.a, c.b, 0, 100); got != c.want {
			t.Errorf("%s：ScrollNearest(%v, %v, 0, 100) = %v，应为 %v", c.name, c.a, c.b, got, c.want)
		}
	}
}
