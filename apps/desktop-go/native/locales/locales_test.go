package locales

import (
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// 目录由 scripts/gen-native-i18n.mjs 从渲染层目录转换而来，那里已经强制「键对等 + 占位符一致」；
// 这里在 Go 侧再守一遍已入库的产物，让 go test ./native/... 不依赖 Node 也能发现手改或漏提交。

var placeholderRE = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// loadMessages 读一个 ARB 目录，返回 键→消息（不含 @ 元数据与 @@locale）。
func loadMessages(t *testing.T, file string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var all map[string]any
	if err := json.Unmarshal(raw, &all); err != nil {
		t.Fatalf("%s 不是合法 JSON：%v", file, err)
	}
	out := map[string]string{}
	for k, v := range all {
		if strings.HasPrefix(k, "@") {
			continue
		}
		s, ok := v.(string)
		if !ok {
			t.Fatalf("%s：键 %s 的值不是字符串", file, k)
		}
		out[k] = s
	}
	return out
}

// placeholders 取 ICU 消息里的占位符名（ '{' 这种被引起来的字面花括号不算）。
func placeholders(msg string) []string {
	msg = strings.NewReplacer("'{'", "", "'}'", "").Replace(msg)
	seen := map[string]bool{}
	var out []string
	for _, m := range placeholderRE.FindAllStringSubmatch(msg, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	sort.Strings(out)
	return out
}

func TestCatalogsHaveSameKeysAndPlaceholders(t *testing.T) {
	zh := loadMessages(t, "zh-Hans.json")
	en := loadMessages(t, "en.json")
	if len(zh) < 100 {
		t.Fatalf("中文目录只有 %d 条消息，生成产物不完整？", len(zh))
	}
	for k, zhMsg := range zh {
		enMsg, ok := en[k]
		if !ok {
			t.Errorf("en 缺少键 %s", k)
			continue
		}
		if a, b := strings.Join(placeholders(zhMsg), ","), strings.Join(placeholders(enMsg), ","); a != b {
			t.Errorf("键 %s 的占位符不一致：zh [%s]，en [%s]", k, a, b)
		}
	}
	for k := range en {
		if _, ok := zh[k]; !ok {
			t.Errorf("en 多出源语言没有的键 %s", k)
		}
	}
}

func TestPlaceholders(t *testing.T) {
	cases := []struct {
		msg  string
		want string
	}{
		{"无占位符", ""},
		{"共 {count} 条，进行中 {active}", "active,count"},
		{"重复 {n} 和 {n}", "n"},
		{"字面花括号 '{'不是占位符'}'", ""},
	}
	for _, c := range cases {
		if got := strings.Join(placeholders(c.msg), ","); got != c.want {
			t.Errorf("%q：期望 [%s]，实际 [%s]", c.msg, c.want, got)
		}
	}
}
