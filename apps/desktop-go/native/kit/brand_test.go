package kit

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"sort"
	"testing"

	"github.com/egoist/mygo/ui"

	"guizhi-mygo-shell/native/kit/icons"
)

// inkPixels 数出画面里与背景色明显不同的像素：用来确认一枚图标真的画出了东西（而不是解析成空）。
func inkPixels(img *image.RGBA, bg ui.Color) int {
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			p := img.RGBAAt(x, y)
			if absDiff(p.R, bg.R) > 24 || absDiff(p.G, bg.G) > 24 || absDiff(p.B, bg.B) > 24 {
				n++
			}
		}
	}
	return n
}

func absDiff(a, b uint8) uint8 {
	if a > b {
		return a - b
	}
	return b - a
}

// drawOne 把 build 画在 size+16 见方的纯色背景上，返回画面。
func drawOne(l *Look, size float32, build func(c *ui.Context)) *image.RGBA {
	w := int(size) + 16
	tr := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(l.Theme)
		ui.Column(c).Fill().Center().Background(l.T.Background).Children(func() { build(c) })
	}, w, w)
	tr.Frame()
	return tr.Image()
}

func TestPlatformTableIsDrawable(t *testing.T) {
	if len(platformTable) < 10 {
		t.Fatalf("平台表只有 %d 项，生成产物不完整？", len(platformTable))
	}
	l := NewLook(DefaultThemeSpec(), Env{})
	ids := make([]string, 0, len(platformTable)+1)
	for id := range platformTable {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	ids = append(ids, "no-such-platform") // 本版本不认识的值：走「未知来源」，不能 panic
	for _, id := range ids {
		if PlatformLabel(id) == "" {
			t.Errorf("平台 %s 的名称为空", id)
		}
		img := drawOne(l, 24, func(c *ui.Context) { PlatformIcon(c, l, id, 24, l.T.Foreground) })
		if n := inkPixels(img, l.T.Background); n < 30 {
			t.Errorf("平台 %s 的图标几乎什么都没画出来（%d 个像素）", id, n)
		}
	}
	if PlatformLabel("no-such-platform") != PlatformLabel("") {
		t.Error("未知平台应统一显示为「未知来源」")
	}
}

func TestEveryBrandParses(t *testing.T) {
	brands := icons.Brands()
	if len(brands) < 20 {
		t.Fatalf("品牌图标只有 %d 个", len(brands))
	}
	for _, b := range brands {
		svg := icons.BrandSVG(b)
		bm, frac := icons.BrandBitmap(b)
		switch {
		case svg == nil && bm == nil:
			t.Errorf("品牌图标 %s 既解析不出矢量也解码不出位图", b)
		case svg != nil && bm != nil:
			t.Errorf("品牌图标 %s 同时是矢量和位图", b)
		case bm != nil && (frac <= 0 || frac > 1):
			t.Errorf("位图品牌图标 %s 的画布占比异常：%v", b, frac)
		}
	}
	if icons.BrandSVG("nope") != nil {
		t.Error("未知键应返回 nil")
	}
}

func TestModelIconRules(t *testing.T) {
	cases := map[string]bool{
		"GPT":           true,
		"Claude":        true,
		"Hunyuan":       true, // 与 Spark 共用腾讯云图标
		"Custom":        true,
		"Other":         true,
		"ERNIE":         false, // 渲染层里是空占位：走字母兜底
		"Unknown":       false,
		"":              false,
		"nanobananai 🍌": false, // 香蕉分类有自己的图标，但 hasDedicatedCategoryIcon 对它是 false
	}
	for cat, want := range cases {
		if got := HasModelIcon(cat); got != want {
			t.Errorf("HasModelIcon(%q) = %v，期望 %v", cat, got, want)
		}
	}
}

func TestModelIconsAreDrawable(t *testing.T) {
	l := NewLook(DefaultThemeSpec(), Env{})
	cats := []string{"nanobananai 🍌", "ERNIE", "", "Unknown", "Custom", "Other"}
	for c := range modelBrandByCategory {
		cats = append(cats, c)
	}
	for _, cat := range cats {
		for _, size := range []float32{20, 40} {
			img := drawOne(l, size, func(c *ui.Context) { ModelIcon(c, l, cat, size) })
			if n := inkPixels(img, l.T.Background); n < int(size) {
				t.Errorf("分类 %q 在 %v 尺寸下几乎什么都没画出来（%d 个像素）", cat, size, n)
			}
		}
	}
}

// TestBrandSheet 在设置了 GUIZHI_BRAND_SHEET=前缀 时，把全部品牌图标画成明、暗两张联系表（前缀-light.png / 前缀-dark.png），
// 供人眼核对；平时跳过。
func TestBrandSheet(t *testing.T) {
	prefix := os.Getenv("GUIZHI_BRAND_SHEET")
	if prefix == "" {
		t.Skip("未设置 GUIZHI_BRAND_SHEET")
	}
	for _, dark := range []bool{false, true} {
		spec := DefaultThemeSpec()
		spec.Mode = ModeLight
		name := "light"
		if dark {
			spec.Mode, name = ModeDark, "dark"
		}
		l := NewLook(spec, Env{})

		platforms := make([]string, 0, len(platformTable)+1)
		for id := range platformTable {
			platforms = append(platforms, id)
		}
		sort.Strings(platforms)
		platforms = append(platforms, "unknown")
		cats := []string{"nanobananai 🍌", "ERNIE", "Custom", "Other"}
		for c := range modelBrandByCategory {
			cats = append(cats, c)
		}
		sort.Strings(cats)

		tr := ui.NewTester(func(c *ui.Context) {
			c.SetTheme(l.Theme)
			ui.Column(c).Fill().Background(l.T.Background).Padding(12).Gap(12).Children(func() {
				ui.Row(c).Gap(14).AlignItems(ui.Center).Children(func() {
					for _, id := range platforms {
						PlatformIcon(c, l, id, 28, l.T.MutedForeground)
					}
				})
				for _, size := range []float32{20, 40, 64} {
					ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
						for _, cat := range cats {
							ModelIcon(c, l, cat, size)
						}
					})
				}
			})
		}, 840, 230)
		tr.Frame()
		f, err := os.Create(fmt.Sprintf("%s-%s.png", prefix, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, tr.Image()); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
