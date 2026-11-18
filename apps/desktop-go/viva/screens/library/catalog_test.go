package library

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"

	"guizhi-mygo-shell/viva/tokens"
)

// 目录与渲染层的对拍：平台顺序、标签颜色、图标目录都抄自渲染层，源文件改了这里先红。

func repoFile(t *testing.T, parts ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{"..", "..", "..", "..", ".."}, parts...)...)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败：%v", path, err)
	}
	return string(raw)
}

var quoted = regexp.MustCompile(`"([^"]+)"`)

// stringList 取 `export const NAME = [ ... ]` 里的字符串字面量。
func stringList(t *testing.T, src, name string) []string {
	t.Helper()
	block := regexp.MustCompile(`(?s)` + name + `\s*=\s*\[(.*?)\]`).FindStringSubmatch(src)
	if block == nil {
		t.Fatalf("找不到 %s 的定义，写法变了就同步改这条测试", name)
	}
	var out []string
	for _, m := range quoted.FindAllStringSubmatch(block[1], -1) {
		out = append(out, m[1])
	}
	return out
}

func TestSourcePlatformsMatchSharedSource(t *testing.T) {
	want := stringList(t, repoFile(t, "packages", "shared", "utils", "source-platforms.ts"), "SOURCE_PLATFORMS")
	if !reflect.DeepEqual(SourcePlatforms, want) {
		t.Errorf("平台顺序 %v，源文件是 %v", SourcePlatforms, want)
	}
}

func TestTagColorsMatchRenderer(t *testing.T) {
	keys := stringList(t, repoFile(t, "packages", "shared", "types", "knowledge.ts"), "TAG_COLOR_KEYS")
	var got []string
	for _, c := range tagColors() {
		got = append(got, c.Key)
	}
	if !reflect.DeepEqual(got, keys) {
		t.Fatalf("标签颜色顺序 %v，源文件是 %v", got, keys)
	}
	// 色点：TAG_DOT_CLASSES 里每个键的 bg-<色>-<档> 就是 Tailwind 调色板里同名的一档。
	meta := repoFile(t, "apps", "desktop", "src", "renderer", "components", "library", "type-meta.tsx")
	block := regexp.MustCompile(`(?s)TAG_DOT_CLASSES[^=]*=\s*\{(.*?)\n\};`).FindStringSubmatch(meta)
	if block == nil {
		t.Fatal("找不到 TAG_DOT_CLASSES 的定义，写法变了就同步改这条测试")
	}
	dots := map[string]string{}
	for _, m := range regexp.MustCompile(`(\w+):\s*"bg-([a-z]+-\d+)"`).FindAllStringSubmatch(block[1], -1) {
		dots[m[1]] = m[2]
	}
	if len(dots) != len(keys) {
		t.Fatalf("只解析出 %d 个色点，期望 %d 个", len(dots), len(keys))
	}
	for _, key := range keys {
		if tagDotColor(key) != tokens.Tailwind(dots[key]) {
			t.Errorf("标签颜色 %s 的色点应是 %s", key, dots[key])
		}
	}
}

func TestUnknownTagColorHasNoDot(t *testing.T) {
	if got := tagDotColor("chartreuse"); got != (tokens.Color{}) {
		t.Errorf("未知颜色的色点应是零值（渲染层里没有底色类），得到 %+v", got)
	}
}

func TestCollectionIconCatalogMatchesRenderer(t *testing.T) {
	src := repoFile(t, "apps", "desktop", "src", "renderer", "components", "library", "collection-icons.ts")
	groups := regexp.MustCompile(`(?s)id:\s*"(\w+)",\s*labelKey:\s*"([\w.]+)",\s*fallback:\s*"([^"]+)",\s*icons:\s*\[(.*?)\]`).FindAllStringSubmatch(src, -1)
	got := collectionIconGroups()
	if len(groups) != len(got) {
		t.Fatalf("源文件有 %d 组，这里有 %d 组", len(groups), len(got))
	}
	seen := map[string]string{}
	for i, g := range groups {
		if got[i].ID != g[1] {
			t.Errorf("第 %d 组：id %q，源文件是 %q", i+1, got[i].ID, g[1])
		}
		if label := got[i].Label(); label != g[3] {
			t.Errorf("%s 组名称 %q，源文件的缺省文案是 %q", g[1], label, g[3])
		}
		var icons []string
		for _, m := range quoted.FindAllStringSubmatch(g[4], -1) {
			icons = append(icons, m[1])
		}
		if !reflect.DeepEqual(got[i].Icons, icons) {
			t.Errorf("%s 组图标 %v，源文件是 %v", g[1], got[i].Icons, icons)
		}
		for _, icon := range got[i].Icons {
			if prev, dup := seen[icon]; dup {
				t.Errorf("图标 %s 在 %s 与 %s 里重复：选中时会有两格同时高亮", icon, prev, g[1])
			}
			seen[icon] = g[1]
		}
	}
}

func TestIsCatalogIcon(t *testing.T) {
	if !isCatalogIcon("📚") || !isCatalogIcon("❗") {
		t.Error("目录里的图标应当被认出")
	}
	for _, icon := range []string{"", "folder", "🦄"} {
		if isCatalogIcon(icon) {
			t.Errorf("%q 不在目录里", icon)
		}
	}
}
