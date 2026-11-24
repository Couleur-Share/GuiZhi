package services

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// providerOptionPattern 取渲染层 constants.ts 里 PROVIDER_OPTIONS 每一项的 id 与 iconCategory（对象字面量里 id 在前、iconCategory 在后）。
var providerOptionPattern = regexp.MustCompile(`(?s)\{\s*id:\s*"([^"]+)".*?iconCategory:\s*"([^"]+)"`)

// TestProviderPresetIconCategoryMatchesRenderer 核对供应商预设的图标分类与渲染层 PROVIDER_OPTIONS 逐项一致（台账 B-075）：
// 图标分类决定供应商下拉、端点卡片与模型行用哪家品牌的图标，错一项就是错一家的 logo。
// 预设集合（id 与顺序）也一并核对：渲染层加减供应商时这里必须同步。
func TestProviderPresetIconCategoryMatchesRenderer(t *testing.T) {
	path := filepath.Join("..", "..", "..", "desktop", "src", "renderer", "components", "settings", "ai-workbench", "constants.ts")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取渲染层 constants.ts 失败：%v", err)
	}
	matches := providerOptionPattern.FindAllStringSubmatch(string(src), -1)
	presets := AIProviderPresets()
	if len(matches) == 0 {
		t.Fatal("没有从 constants.ts 解析到任何供应商项（解析规则失效？）")
	}
	if len(matches) != len(presets) {
		t.Fatalf("渲染层有 %d 个供应商预设，Go 侧有 %d 个", len(matches), len(presets))
	}
	for i, m := range matches {
		id, category := m[1], m[2]
		got := presets[i]
		if got.ID != id {
			t.Errorf("第 %d 项：渲染层是 %q，Go 是 %q（顺序即下拉顺序）", i, id, got.ID)
			continue
		}
		if got.IconCategory != category {
			t.Errorf("供应商 %q 的图标分类：渲染层 %q，Go %q", id, category, got.IconCategory)
		}
		if ProviderIconCategory(id) != category {
			t.Errorf("ProviderIconCategory(%q) = %q，渲染层 %q", id, ProviderIconCategory(id), category)
		}
	}
}
