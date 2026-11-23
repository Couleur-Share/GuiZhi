package overlays

import (
	"os"
	"regexp"
	"testing"
)

// 设置分区的检索关键词与渲染层 GlobalCommandPalette.tsx 的 SETTINGS 逐项一致：渲染层改了关键词，这里必须同步，
// 否则中文用户在原生版里搜不到原来能搜到的设置分区。渲染层源码不在检出里（比如只拿到 Go 目录）时跳过。
func TestPaletteSettingsKeywordsMatchRenderer(t *testing.T) {
	src, err := os.ReadFile("../../../../desktop/src/renderer/components/app/GlobalCommandPalette.tsx")
	if err != nil {
		t.Skipf("渲染层源码不可用：%v", err)
	}
	re := regexp.MustCompile(`id:\s*"(\w+)"[^\n]*?keywords:\s*"([^"]*)"`)
	found := map[string]string{}
	for _, m := range re.FindAllStringSubmatch(string(src), -1) {
		found[m[1]] = m[2]
	}
	if len(found) != len(paletteSettings) {
		t.Fatalf("渲染层有 %d 个设置分区，这里有 %d 个：%v", len(found), len(paletteSettings), found)
	}
	for _, s := range paletteSettings {
		want, ok := found[s.id]
		if !ok {
			t.Errorf("渲染层没有设置分区 %q", s.id)
			continue
		}
		if got := settingsKeywords[s.id]; got != want {
			t.Errorf("分区 %q 的关键词与渲染层不一致：\n got %q\nwant %q", s.id, got, want)
		}
	}
}

func TestPaletteSettingsKeywordsCoverEverySection(t *testing.T) {
	for _, s := range paletteSettings {
		if settingsKeywords[s.id] == "" {
			t.Errorf("分区 %q 没有关键词", s.id)
		}
	}
}
