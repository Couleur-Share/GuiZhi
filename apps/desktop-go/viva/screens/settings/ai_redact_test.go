package settings

import (
	"strings"
	"testing"
)

// redact 是错误文字到达界面之前的最后一道脱敏：每一处出现都要抹掉，不是只抹第一处；
// 与密钥无关的短词（不足 4 个字符）不能被当成密钥抹掉，空白密钥不参与。
func TestRedactReplacesEveryOccurrence(t *testing.T) {
	const key = "sk-live-9f2c-SECRET-7d41"

	got := redact("first "+key+" then "+key+" again", key)
	if strings.Contains(got, key) || got != "first *** then *** again" {
		t.Errorf("密钥出现两次应都被抹掉：%q", got)
	}

	// 多个密钥各自抹掉；密钥两端的空白先去掉再匹配。
	if got := redact("a KEY1234 b TOK5678 c KEY1234", " KEY1234 ", "TOK5678"); got != "a *** b *** c ***" {
		t.Errorf("多个密钥：%q", got)
	}

	// 长度边界：4 个字符是下限。
	if got := redact("pass abcd abc", "abcd"); got != "pass *** abc" {
		t.Errorf("4 个字符的密钥应被抹掉：%q", got)
	}
	if got := redact("abc abc", "abc"); got != "abc abc" {
		t.Errorf("不足 4 个字符不应被当成密钥抹掉：%q", got)
	}

	// 空白或空串不参与替换（否则整段文字会被「***」切碎）。
	if got := redact("nothing to hide", "", "   "); got != "nothing to hide" {
		t.Errorf("空密钥不应改动文字：%q", got)
	}
}
