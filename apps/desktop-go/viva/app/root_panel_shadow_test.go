package app

import (
	"image"
	"testing"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/screens/shell"
	"guizhi-mygo-shell/viva/tokens"
)

// maxChannelDelta 是两个像素在 R、G、B 三个通道上的最大差（对照工具量墨迹与阴影用的同一个口径）。
func maxChannelDelta(img *image.RGBA, x1, y1, x2, y2 int) int {
	a, b := img.RGBAAt(x1, y1), img.RGBAAt(x2, y2)
	d := 0
	for _, p := range [][2]uint8{{a.R, b.R}, {a.G, b.G}, {a.B, b.B}} {
		diff := int(p[0]) - int(p[1])
		if diff < 0 {
			diff = -diff
		}
		d = max(d, diff)
	}
	return d
}

// 台账 B-046 (7)(c)：二级侧栏（渲染层的 aside 是 z-20 的定位元素）的外阴影投在右边的工作区上，
// 侧栏右缘外的像素列比工作区底色暗。读数取自渲染层 table 场景（royal-blue、大档、1280×800）在侧栏右缘逐像素读得的最大通道差：
// 浅色主题（0 1px 2px 0 rgba(0,0,0,.04)）是 3、1，深色主题（0 2px 4px 0 rgba(0,0,0,.4)）是 4、2、1；
// 这之后的列与工作区底色相同。侧栏自己画在工作区之前，阴影要放在整行画完之后补，否则被工作区盖住。
func TestRootPanelEdgeShadowFallsOnTheWorkspace(t *testing.T) {
	for _, c := range []struct {
		name string
		dark bool
		want []int // 侧栏右缘外第 0、1、2…列相对工作区底色的最大通道差；列表之后的列必须为 0
	}{
		{"浅色", false, []int{3, 1}},
		{"深色", true, []int{4, 2, 1}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := openRoot(t, func(o *Options) {
				o.Page, o.Module = shell.PageHome, shell.ModuleImports
				if c.dark {
					o.Appearance = func(s kit.Spec) kit.Spec { s.Mode, s.Motion = tokens.ModeDark, kit.MotionLevelOff; return s }
				}
			})
			img, err := f.session.Image()
			if err != nil {
				t.Fatal(err)
			}
			def, _, _ := shell.PanelBounds()
			edge := int(f.root.Look().Rem(5) + def) // rail（5rem）加二级侧栏宽度：侧栏右缘的像素位置
			const y = 700                           // 工作区下半部分：没有内容，只有页面底色
			far := edge + 40
			for i := 0; i < 8; i++ {
				want := 0
				if i < len(c.want) {
					want = c.want[i]
				}
				if got := maxChannelDelta(img, edge+i, y, far, y); got != want {
					t.Errorf("侧栏右缘外第 %d 列（x=%d）比工作区底色暗 %d 个灰阶，期望 %d", i, edge+i, got, want)
				}
			}
			// 沿侧栏的高度都有（窗格两端的 2 个模糊半径里按高斯定义渐变，那一段不比）：取窗格中段的两行。
			for _, row := range []int{250, 550} {
				if got := maxChannelDelta(img, edge, row, far, row); got != c.want[0] {
					t.Errorf("y=%d 处侧栏右缘外第 0 列暗 %d 个灰阶，期望沿窗格高度都是 %d", row, got, c.want[0])
				}
			}
		})
	}
}

// 台账 G-019：二级侧栏的底色只有 aside 的 panel-strong 令牌——渲染层的 `bg-sidebar-background/85` 在 tailwind 里没有对应的颜色，
// 不生成任何样式。曾按类名多画一层 Sidebar@85%，深色主题下整块侧栏比渲染层暗 6–7 个灰阶，L3 的逐通道容差 12 把它放过了。
// 深色的 panel-strong 是不透明色，侧栏里没有内容的地方必须恰好是这个颜色。
func TestRootPanelBackgroundIsOnlyThePanelStrongToken(t *testing.T) {
	f := openRoot(t, func(o *Options) {
		o.Page, o.Module = shell.PageHome, shell.ModuleImports
		o.Appearance = func(s kit.Spec) kit.Spec { s.Mode, s.Motion = tokens.ModeDark, kit.MotionLevelOff; return s }
	})
	img, err := f.session.Image()
	if err != nil {
		t.Fatal(err)
	}
	panel := f.root.Look().T.UiPanelStrongBg
	if panel.A != 1 {
		t.Fatalf("前提不成立：深色的 panel-strong 应是不透明色，实际 alpha %v", panel.A)
	}
	// 侧栏下半部分的空白处（没有行、没有边线）。
	got := img.RGBAAt(60+int(f.root.Look().Rem(5)), 700)
	want := [3]uint8{panel.R, panel.G, panel.B}
	if got.R != want[0] || got.G != want[1] || got.B != want[2] {
		t.Errorf("侧栏空白处像素 (%d,%d,%d)，期望恰好是 panel-strong 令牌 (%d,%d,%d)", got.R, got.G, got.B, want[0], want[1], want[2])
	}
}
