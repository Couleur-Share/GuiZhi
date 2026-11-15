package settings

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// 渲染层 localStorage 键的登记核对。
//
// 每周合并 main 时，渲染层可能新增 localStorage 键。没登记进 legacyExact / legacyPrefixes 的键，
// 切换后会悄悄丢失（首次启动导入只认登记过的键）。本测试扫描渲染层源码里的 localStorage 用法，
// 发现没登记的键就失败；反过来，登记了但源码里已经没有的键也要清理。
// 切换后渲染层目录不存在，测试自动跳过。
//
// 能识别的写法：字面量键、`const X = "字面量"` 常量（先找同文件，再找全局唯一值）、模板字面量（取插值前的静态前缀）、
// zustand persist 选项里的 name、配置迁移的 TRANSFER_LOCAL_STORAGE_KEYS 白名单。
// 识别不了的写法（变量来源不明）会失败，要求显式登记到 dynamicKeyUses 或改写成可识别的形式。

// 三种字符串写法各占一个捕获组：双引号、单引号、模板字面量。
const stringLit = "(?:\"([^\"]*)\"|'([^']*)'|`([^`]*)`)"

var (
	constDecl   = regexp.MustCompile(`(?:const|let|var)\s+([A-Za-z_]\w*)\s*(?::\s*[A-Za-z]+\s*)?=\s*` + stringLit)
	storageCall = regexp.MustCompile(`localStorage\.(?:getItem|setItem|removeItem)\(\s*(?:` + stringLit + `|([A-Za-z_]\w*))`)
	persistName = regexp.MustCompile(`\{\s*name:\s*["']([^"']+)["']\s*,\s*(?:version|partialize|merge|migrate|storage|onRehydrateStorage|skipHydration)\b`)
	transferKey = regexp.MustCompile(`TRANSFER_LOCAL_STORAGE_KEYS\s*=\s*\[([^\]]*)\]`)
	quotedKey   = regexp.MustCompile(`["']([^"']+)["']`)
)

// dynamicKeyUses 登记按设计用变量传键的位置（文件名 → 变量名 → 键的来源说明）。
var dynamicKeyUses = map[string]map[string]string{
	"config-local-storage.ts": {"key": "遍历 TRANSFER_LOCAL_STORAGE_KEYS（shared/utils/config-transfer.ts，本测试单独解析）"},
}

// literalOf 取三个字面量捕获组里非空的那个；模板字面量只要插值之前的静态前缀。
func literalOf(groups []string) string {
	for _, g := range groups {
		if g != "" {
			return strings.SplitN(g, "${", 2)[0]
		}
	}
	return ""
}

func TestRendererLocalStorageKeysAreAllRegistered(t *testing.T) {
	repoRoot := filepath.Join("..", "..", "..", "..")
	root := filepath.Join(repoRoot, "apps", "desktop", "src", "renderer")
	if _, err := os.Stat(root); err != nil {
		t.Skip("渲染层源码不在（已切换到原生界面）")
	}
	exact, prefixes := KnownLegacyKeys()
	known := map[string]bool{}
	for _, k := range exact {
		known[k] = true
	}
	registered := func(key string) bool {
		if known[key] {
			return true
		}
		for _, p := range prefixes {
			if strings.HasPrefix(key, p) {
				return true
			}
		}
		return false
	}

	type source struct{ name, text string }
	var sources []source
	fileConsts := map[string]map[string]string{} // 文件名 → 常量名 → 值（路径重名的文件按完整路径区分）
	globalConsts := map[string]map[string]bool{} // 常量名 → 出现过的值
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !(strings.HasSuffix(path, ".ts") || strings.HasSuffix(path, ".tsx")) {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(b)
		sources = append(sources, source{path, text})
		local := map[string]string{}
		for _, m := range constDecl.FindAllStringSubmatch(text, -1) {
			if v := literalOf(m[2:5]); v != "" {
				local[m[1]] = v
				if globalConsts[m[1]] == nil {
					globalConsts[m[1]] = map[string]bool{}
				}
				globalConsts[m[1]][v] = true
			}
		}
		fileConsts[path] = local
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	found := map[string][]string{} // 键（或动态键的静态前缀）→ 出现的文件
	persisted := map[string]bool{}
	for _, s := range sources {
		base := filepath.Base(s.name)
		add := func(key string) { found[key] = append(found[key], base) }
		for _, m := range storageCall.FindAllStringSubmatch(s.text, -1) {
			if key := literalOf(m[1:4]); key != "" {
				add(key)
				continue
			}
			ident := m[4]
			if ident == "" {
				t.Errorf("%s：localStorage 的键以插值开头，无法识别静态前缀", base)
				continue
			}
			// 解析顺序：显式登记的动态用法 → 同文件常量 → 从别处 import 进来的常量（全局取值唯一才认）。
			if _, ok := dynamicKeyUses[base][ident]; ok {
				continue
			}
			if v, ok := fileConsts[s.name][ident]; ok {
				add(v)
				continue
			}
			vals := globalConsts[ident]
			imported := regexp.MustCompile(`import\s*(?:type\s*)?\{[^}]*\b` + regexp.QuoteMeta(ident) + `\b[^}]*\}\s*from`).MatchString(s.text)
			if imported && len(vals) == 1 {
				for v := range vals {
					add(v)
				}
				continue
			}
			t.Errorf("%s：localStorage 的键用了变量 %s，来源不明（import=%v，全局取值 %d 种）——"+
				"改成可识别的写法，或登记到 dynamicKeyUses 并说明键从哪来", base, ident, imported, len(vals))
		}
		for _, m := range persistName.FindAllStringSubmatch(s.text, -1) {
			add(m[1])
			persisted[m[1]] = true
		}
	}

	// 配置迁移白名单里的键（config-local-storage.ts 经变量读写它们）。
	transferFile := filepath.Join(repoRoot, "packages", "shared", "utils", "config-transfer.ts")
	if b, err := os.ReadFile(transferFile); err != nil {
		t.Errorf("读不到 %s：%v（白名单位置变了？更新本测试）", transferFile, err)
	} else if m := transferKey.FindSubmatch(b); m == nil {
		t.Errorf("%s 里找不到 TRANSFER_LOCAL_STORAGE_KEYS 数组", transferFile)
	} else {
		for _, q := range quotedKey.FindAllSubmatch(m[1], -1) {
			found[string(q[1])] = append(found[string(q[1])], "config-transfer.ts")
		}
	}

	// 扫描规则自检：数量与两个持久化 store 都必须被识别，否则说明规则失效了，不能静默通过。
	if len(found) < 12 {
		t.Fatalf("只扫到 %d 个键，扫描规则可能失效了：%v", len(found), found)
	}
	for _, want := range []string{LegacySettingsKey, "ui-storage"} {
		if !persisted[want] {
			t.Errorf("没有从 persist 选项里识别到 %q，persist 的写法变了？更新 persistName", want)
		}
	}

	var missing []string
	for key, files := range found {
		if !registered(key) {
			missing = append(missing, fmt.Sprintf("%q（%s）", key, strings.Join(files, "、")))
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("渲染层新增了 native/settings/legacy.go 没登记的 localStorage 键：\n  %s\n"+
			"请决定它们在原生版里的归宿（偏好 / 设置 / 有意忽略），登记后补解析与测试", strings.Join(missing, "\n  "))
	}

	// 反向：登记了但渲染层已经不用的键要清理，否则登记表会越攒越旧。
	for _, key := range exact {
		if key == "guizhi-legacy-ls-imported" { // Electron 导入标记，由 Go 壳写入，不在渲染层源码里
			continue
		}
		if _, ok := found[key]; !ok {
			t.Errorf("legacy.go 登记了 %q，但渲染层源码里已经没有它：确认后从登记表移除", key)
		}
	}
	for _, p := range prefixes {
		used := false
		for key := range found {
			if strings.HasPrefix(key, p) {
				used = true
			}
		}
		if !used {
			t.Errorf("legacy.go 登记了前缀 %q，但渲染层源码里已经没有它：确认后从登记表移除", p)
		}
	}
}
