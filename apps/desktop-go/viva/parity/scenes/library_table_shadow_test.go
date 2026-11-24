package scenes_test

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/parity"
	"guizhi-mygo-shell/viva/parity/scenes"
	"guizhi-mygo-shell/viva/screens/shell"
)

// rgbaAt 取图片在 (x, y) 的像素（对照用的无头画布是 *image.RGBA）。
func rgbaAt(t *testing.T, img image.Image, x, y int) color.RGBA {
	t.Helper()
	rgba, ok := img.(*image.RGBA)
	if !ok {
		t.Fatalf("无头画布不是 *image.RGBA：%T", img)
	}
	return rgba.RGBAAt(x, y)
}

// maxDelta 是两个像素在 R、G、B 三个通道上的最大差。
func maxDelta(a, b color.RGBA) int {
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

// 台账 B-048：知识库表格横向滚动后，吸附表头（thead，渲染层 z-30）盖在二级侧栏（aside，z-20）的外阴影之上，
// 表头那一段的右缘外是纯表头底色；数据行那一段仍有侧栏阴影（B-046 (7)(c)：表格场景里浅色 3、1，深色 5、3、1 个灰阶（Viva 与渲染层的数据行逐像素相同，见台账 B-048 的实测）：\n// 表头行里这几列应当为 0）。
// 此前 Viva 的补画（整行的 DrawOver，晚于一切子元素）把阴影画在表头之上，表头行右缘外多出 3、1（浅）或 5、3、1、1（深）个灰阶。
// 这里直接取 table-scrolled 场景的无头渲染像素：表头行右缘外各列必须恰好等于更靠右的表头底色，数据行右缘外必须保留阴影。
func TestTableScrolledHeadCoversThePanelEdgeShadow(t *testing.T) {
	scene, err := scenes.Get("app.window.library.table-scrolled")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		mode string
		font string
		want []int // 数据行：侧栏右缘外第 0、1、2…列相对更靠右的工作区底色的最大通道差；列表之后的列为 0
	}{
		{"浅色中档", "light", "medium", []int{3, 1}},
		{"深色中档", "dark", "medium", []int{5, 3, 1}},
		{"浅色大档", "light", "large", []int{3, 1}},
		{"深色大档", "dark", "large", []int{5, 3, 1}},
	} {
		t.Run(c.name, func(t *testing.T) {
			v := parity.Variant{Mode: c.mode, Hue: "royal-blue", Font: c.font, Viewport: "1280x800"}
			facts, img, err := parity.Render(scene, v, parity.Region{Width: 1280, Height: 800})
			if err != nil {
				t.Fatal(err)
			}
			// 侧栏右缘：rail（5rem）加二级侧栏的默认宽度；表头行的纵向位置取自可访问树里第一条列宽拖动把手（占满表头行的高度）。
			def, _, _ := shell.PanelBounds()
			edge := int(kit.NewLook(v.Spec()).Rem(5) + def)
			headY := 0
			for _, n := range facts.Nodes {
				if box, ok := n.BoxOK(); ok && n.Role == "separator" && strings.HasPrefix(n.Name, "调整「") {
					headY = int(box.Y() + box.H()/2)
					break
				}
			}
			if headY == 0 {
				t.Fatal("前提不成立：没找到表头的列宽拖动把手")
			}
			far := edge + 40
			// 前提：表头压在侧栏右缘外（横向滚动过），表头行远处与右缘外相邻的像素都应是表头底色。
			for i := 0; i < 8; i++ {
				if got := maxDelta(rgbaAt(t, img, edge+i, headY), rgbaAt(t, img, far, headY)); got != 0 {
					t.Errorf("表头行 y=%d：侧栏右缘外第 %d 列（x=%d）比表头底色差 %d 个灰阶，期望 0（表头盖住侧栏阴影）", headY, i, edge+i, got)
				}
			}
			// 数据行（窗格中段，没有表头压着）：阴影照旧。
			const dataY = 300
			for i := 0; i < 8; i++ {
				want := 0
				if i < len(c.want) {
					want = c.want[i]
				}
				if got := maxDelta(rgbaAt(t, img, edge+i, dataY), rgbaAt(t, img, far, dataY)); got != want {
					t.Errorf("数据行 y=%d：侧栏右缘外第 %d 列（x=%d）比工作区底色差 %d 个灰阶，期望 %d", dataY, i, edge+i, got, want)
				}
			}
		})
	}
}
