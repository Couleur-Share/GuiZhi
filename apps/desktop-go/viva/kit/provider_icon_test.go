package kit

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
	"unsafe"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// 母版：15 张 PNG 都在、都是 120×120、都有可见像素（不是一张空图）。
func TestProviderIconMastersAreEmbeddedAndNonEmpty(t *testing.T) {
	if len(providerIconNames) != 15 {
		t.Fatalf("品牌图标应有 15 张，实际 %d", len(providerIconNames))
	}
	entries, err := providerIconFS.ReadDir("assets/providers")
	if err != nil || len(entries) != len(providerIconNames) {
		t.Fatalf("内嵌目录应恰好有 %d 张：%d，%v", len(providerIconNames), len(entries), err)
	}
	for _, name := range providerIconNames {
		bmp, ok := providerIconBitmap(name, providerMasterSize)
		if !ok || bmp.Width != providerMasterSize || bmp.Height != providerMasterSize {
			t.Errorf("%s：母版缺失或尺寸不对：%v %dx%d", name, ok, bmp.Width, bmp.Height)
			continue
		}
		var opaque int
		for i := 3; i < len(bmp.Pix); i += 4 {
			if bmp.Pix[i] > 128 {
				opaque++
			}
		}
		if opaque < 200 {
			t.Errorf("%s：可见像素只有 %d 个，疑似空图", name, opaque)
		}
	}
}

// 分类表与渲染层 ModelIcons.tsx 的 CATEGORY_ICON_SRC 逐项一致（含 Spark / Hunyuan 共用腾讯图标、ERNIE 为空）。
func TestProviderIconTableMatchesModelIconsSource(t *testing.T) {
	src, err := os.ReadFile(filepath.FromSlash("../../../desktop/src/renderer/components/ui/ModelIcons.tsx"))
	if err != nil {
		t.Skipf("读不到渲染层源码：%v", err)
	}
	text := string(src)
	// import xxxSvg from ".../providers/yyy.svg";
	imports := map[string]string{}
	for _, m := range regexp.MustCompile(`import (\w+) from "[^"]*/providers/([\w-]+)\.svg"`).FindAllStringSubmatch(text, -1) {
		imports[m[1]] = m[2]
	}
	block := regexp.MustCompile(`(?s)CATEGORY_ICON_SRC: Record<string, string> = \{(.*?)\n\};`).FindStringSubmatch(text)
	if block == nil {
		t.Fatal("没找到 CATEGORY_ICON_SRC")
	}
	want := map[string]string{}
	for _, m := range regexp.MustCompile(`(?m)^\s*(?:"([^"]+)"|(\w+)):\s*(\w+|""),`).FindAllStringSubmatch(block[1], -1) {
		category := m[1]
		if category == "" {
			category = m[2]
		}
		if m[3] == `""` {
			continue // 渲染层没有图标：Go 侧同样不登记
		}
		file, ok := imports[m[3]]
		if !ok {
			t.Fatalf("分类 %s 引用了未 import 的 %s", category, m[3])
		}
		want[category] = file
	}
	if len(want) < 16 {
		t.Fatalf("解析出的有图标分类只有 %d 个，解析规则可能失效", len(want))
	}
	for category, file := range want {
		if got := providerIconFiles[category]; got != file {
			t.Errorf("分类 %s：Go 侧 %q，渲染层 %q", category, got, file)
		}
	}
	for category := range providerIconFiles {
		if _, ok := want[category]; !ok {
			t.Errorf("Go 侧多登记了渲染层没有的分类 %s", category)
		}
	}
	// 15 张图都被某个分类用到。
	used := map[string]bool{}
	for _, f := range providerIconFiles {
		used[f] = true
	}
	for _, name := range providerIconNames {
		if !used[name] {
			t.Errorf("%s 没有任何分类使用", name)
		}
	}
	for _, special := range []string{"Custom", "Other"} {
		if _, ok := providerSpecials[special]; !ok || !HasDedicatedProviderIcon(special) {
			t.Errorf("%s 应有专属图标", special)
		}
	}
	if HasDedicatedProviderIcon("ERNIE") || HasDedicatedProviderIcon("") {
		t.Error("ERNIE 与空串不应有专属图标（渲染层 hasDedicatedCategoryIcon 为假）")
	}
}

// 缩小：尺寸正确、同一（品牌，边长）反复取是同一份位图（零拷贝，ID 与 Version 不变）、不同品牌与边长的 ID 互不相同。
func TestProviderIconBitmapCacheAndIDs(t *testing.T) {
	ids := map[uint64]string{}
	for _, name := range providerIconNames {
		for _, px := range []int{16, 20, 24, 32, 40, providerMasterSize} {
			a, ok := providerIconBitmap(name, px)
			if !ok || a.Width != px || a.Height != px || a.Stride != px*4 || len(a.Pix) != px*px*4 {
				t.Fatalf("%s@%d：%v %dx%d", name, px, ok, a.Width, a.Height)
			}
			b, _ := providerIconBitmap(name, px)
			if a.ID != b.ID || a.Version != b.Version || unsafe.SliceData(a.Pix) != unsafe.SliceData(b.Pix) {
				t.Errorf("%s@%d：再取一次应是同一份位图", name, px)
			}
			key := name + "@" + string(rune('0'+px/10)) + string(rune('0'+px%10))
			if prev, dup := ids[a.ID]; dup {
				t.Errorf("位图 ID %#x 重复：%s 与 %s", a.ID, prev, key)
			}
			ids[a.ID] = key
		}
	}
	// 超出母版边长按母版取；非法边长夹到 1。
	if bmp, _ := providerIconBitmap("openai", 999); bmp.Width != providerMasterSize {
		t.Errorf("大边长应取母版：%d", bmp.Width)
	}
	if bmp, _ := providerIconBitmap("openai", 0); bmp.Width != 1 {
		t.Errorf("非法边长应夹到 1：%d", bmp.Width)
	}
	if _, ok := providerIconBitmap("nope", 16); ok {
		t.Error("未知图标不应成功")
	}
}

// 预乘不变量：任何像素的颜色通道不超过 alpha（缩小是在预乘空间里平均）。
func TestProviderIconDownscaleKeepsPremultiplied(t *testing.T) {
	for _, name := range providerIconNames {
		bmp, _ := providerIconBitmap(name, 20)
		for i := 0; i+3 < len(bmp.Pix); i += 4 {
			a := int(bmp.Pix[i+3])
			if int(bmp.Pix[i]) > a+1 || int(bmp.Pix[i+1]) > a+1 || int(bmp.Pix[i+2]) > a+1 {
				t.Fatalf("%s：像素 %d 不是预乘：%v", name, i/4, bmp.Pix[i:i+4])
			}
		}
	}
}

// 缓存有上限：塞满之后整体清空，位图 ID 仍然只由（品牌，边长）决定。
func TestProviderIconSizedCacheIsBounded(t *testing.T) {
	first, _ := providerIconBitmap("grok", 17)
	for px := 1; px <= providerMasterSize; px++ {
		for _, name := range providerIconNames[:3] {
			providerIconBitmap(name, px)
		}
	}
	providerSized.mu.Lock()
	n := len(providerSized.bmp)
	providerSized.mu.Unlock()
	if n > providerSizedCap {
		t.Fatalf("缓存条目 %d 超过上限 %d", n, providerSizedCap)
	}
	again, _ := providerIconBitmap("grok", 17)
	if again.ID != first.ID {
		t.Errorf("清空后再生成的位图 ID 应不变：%#x vs %#x", again.ID, first.ID)
	}
}

// 画出来：品牌 logo、Custom、Other 在给定边长的方块里都有可见像素，且占位正好是 size×size。
func TestProviderBrandIconPaintsInsideItsBox(t *testing.T) {
	categories := []string{
		"GPT", "Azure OpenAI", "Claude", "Gemini", "DeepSeek", "Qwen", "Doubao", "GLM", "Moonshot", "Mistral",
		"Yi", "Spark", "Hunyuan", "New API", "Llama", "Grok", "Custom", "Other",
	}
	for _, size := range []float32{16, 20} {
		var rect gfx.Rect
		var which string
		view := func(l *Look, c ui.C) {
			ui.Column(c).Fill().Pad(24).Children(func() {
				ui.Row(c).Gap(8).Children(func() {
					for _, category := range categories[:len(categories)] {
						if category != which {
							continue
						}
						l.ProviderBrandIcon(c, category, size).Draw(func(d *ui.DrawContext) { rect = d.Rect })
					}
				})
			})
		}
		h := openPage(t, still(), 200, 120, view)
		for _, category := range categories {
			which = category
			h.frames(3)
			if rect.Width != size || rect.Height != size {
				t.Errorf("%s@%g：占位 %gx%g，期望 %gx%g", category, size, rect.Width, rect.Height, size, size)
				continue
			}
			img, err := h.session.Image()
			if err != nil {
				t.Fatal(err)
			}
			bg := img.RGBAAt(2, 2)
			var painted int
			for y := int(rect.Y); y < int(rect.Y+rect.Height); y++ {
				for x := int(rect.X); x < int(rect.X+rect.Width); x++ {
					if img.RGBAAt(x, y) != bg {
						painted++
					}
				}
			}
			if painted < int(size*size)/6 {
				t.Errorf("%s@%g：方块里只画出 %d 个与背景不同的像素", category, size, painted)
			}
		}
	}
}

// 兜底：ERNIE、未知分类与空串退成首字母圆，Custom / Other / 品牌 logo 没有文字节点。
func TestProviderBrandIconLetterFallbackAndTextFreeIcons(t *testing.T) {
	cases := []struct {
		category string
		text     string // 期望出现的文字；空表示不应有任何文字
	}{
		{"ERNIE", "E"}, {"某厂商", "某"}, {"", "?"}, {"nanobananai 🍌", "🍌"},
		{"GPT", ""}, {"Claude", ""}, {"Custom", ""}, {"Other", ""},
	}
	for _, tc := range cases {
		view := func(l *Look, c ui.C) {
			ui.Column(c).Fill().Pad(24).Children(func() { l.ProviderBrandIcon(c, tc.category, 20) })
		}
		h := openPage(t, still(), 120, 80, view)
		var texts []string
		for _, text := range h.session.Snapshot().Texts {
			texts = append(texts, text.Value)
		}
		if tc.text == "" && len(texts) != 0 {
			t.Errorf("%q 不应有文字：%v", tc.category, texts)
		}
		if tc.text != "" && !slices.Contains(texts, tc.text) {
			t.Errorf("%q 应画出 %q：%v", tc.category, tc.text, texts)
		}
	}
}
