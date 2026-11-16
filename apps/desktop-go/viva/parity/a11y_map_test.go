package parity

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

// a11yMapDigest 是归一化规则（忽略的角色、角色映射、状态表……）的规范化哈希。
func a11yMapDigest() string {
	var b strings.Builder
	set := func(name string, m map[string]bool) {
		keys := make([]string, 0, len(m))
		for k, v := range m {
			keys = append(keys, fmt.Sprintf("%q=%v", k, v))
		}
		sort.Strings(keys)
		fmt.Fprintf(&b, "%s:%s\n", name, strings.Join(keys, ","))
	}
	set("reactIgnoredRoles", reactIgnoredRoles)
	set("reactIgnoredUnnamed", reactIgnoredUnnamed)
	set("vivaIgnoredRoles", vivaIgnoredRoles)
	set("popupHasValues", popupHasValues)
	pairs := make([]string, 0, len(reactRoleMap))
	for k, v := range reactRoleMap {
		pairs = append(pairs, k+"→"+v)
	}
	sort.Strings(pairs)
	fmt.Fprintf(&b, "reactRoleMap:%s\n", strings.Join(pairs, ","))
	states := make([]string, 0, len(roleStates))
	for k, v := range roleStates {
		states = append(states, k+"="+strings.Join(v, "|"))
	}
	sort.Strings(states)
	fmt.Fprintf(&b, "roleStates:%s\n", strings.Join(states, ","))
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// TestA11yMapFrozen 守住 L1 的归一化规则：规则一改，摘要就变，测试变红。
// 这些规则决定「什么算结构一致」，不能为了让某个场景通过而放宽（docs/agents/viva-ui-development.md §2.2）。
// 确有必要改时：先在 docs/agents/viva-parity-ledger.md 写明理由，再用新摘要更新 testdata/a11y_map.sha256。
func TestA11yMapFrozen(t *testing.T) {
	want, err := os.ReadFile("testdata/a11y_map.sha256")
	if err != nil {
		t.Fatalf("读不到摘要文件：%v（当前摘要 %s）", err, a11yMapDigest())
	}
	if got := a11yMapDigest(); got != strings.TrimSpace(string(want)) {
		t.Fatalf("a11y 归一化规则变了：摘要 %s，冻结值 %s。先在台账里写明理由，再更新 testdata/a11y_map.sha256", got, strings.TrimSpace(string(want)))
	}
}

func TestNormText(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  开机  自启动\n", "开机 自启动"},
		{"a\u00a0b", "a b"},
		{"é", "é"}, // e + 组合重音符 → 预组合字符
		{"", ""},
	}
	for _, c := range cases {
		if got := NormText(c.in); got != c.want {
			t.Errorf("NormText(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
	if got := SquashText("每 5 分钟 把新条目"); got != "每5分钟把新条目" {
		t.Errorf("SquashText 应去掉全部空白：%q", got)
	}
}
