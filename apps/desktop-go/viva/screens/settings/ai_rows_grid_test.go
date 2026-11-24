package settings

import "testing"

// gridAutoRows 的数值用例（台账 B-076）：数字来自渲染层 800×600 对拍的 facts——
// 空配置侧栏 256、面板 296（侧栏没有下边框，面板滚动区上下内边距 40）；有供应商侧栏 256.5、面板 295.5（侧栏 border-b 的 1px 计入基础高度）。
func TestGridAutoRows(t *testing.T) {
	const big = 10000 // 两行自然高度都远超一半
	for _, tc := range []struct {
		name                      string
		total, a, b, baseA, baseB float32
		wantTop, wantBottom       float32
	}{
		{"空配置 800×600（H=552）", 552, big, big, 0, 40, 256, 296},
		{"有供应商 800×600（H=552）", 552, big, big, 1, 40, 256.5, 295.5},
		{"侧栏没超过一半：保持自然高度", 552, 200, big, 0, 40, 200, 352},
		{"面板没超过一半：侧栏拿走剩下的", 552, big, 180, 1, 40, 372, 180},
		{"两行加起来不到总高：多出来的再平分", 600, 200, 100, 0, 40, 350, 250},
		{"连基础高度都放不下：按基础高度比例分", 30, big, big, 10, 20, 10, 20},
	} {
		top, bottom := gridAutoRows(tc.total, tc.a, tc.b, tc.baseA, tc.baseB)
		if top != tc.wantTop || bottom != tc.wantBottom {
			t.Errorf("%s：得到 %v / %v，应为 %v / %v", tc.name, top, bottom, tc.wantTop, tc.wantBottom)
		}
	}
}
