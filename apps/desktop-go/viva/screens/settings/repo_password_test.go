package settings_test

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/viva/locales"
)

// 恢复口令的门槛是 12 个 UTF-16 码元（渲染层 `password.length < 12`），不是 12 个字节、也不是 12 个字符：
// 汉字一个码元（3 字节），表情符号两个码元（4 字节、1 个字符）。
// 两种换算都会让同一个口令在渲染层与 Viva 里一个能启用、一个不能——口令强度的门槛必须逐位一致。
func TestRepoRecoveryPasswordCountsUTF16Units(t *testing.T) {
	cases := []struct {
		name string
		pw   string
		ok   bool
	}{
		{"11 个 ASCII（差 1 位）", strings.Repeat("a", 11), false},
		{"12 个 ASCII（恰好达标）", strings.Repeat("a", 12), true},
		{"6 个汉字（6 码元、18 字节）", strings.Repeat("归", 6), false},
		{"12 个汉字（12 码元）", strings.Repeat("归", 12), true},
		{"5 个表情（10 码元、5 字符、20 字节）", strings.Repeat("😀", 5), false},
		{"6 个表情（12 码元、仅 6 字符）", strings.Repeat("😀", 6), true},
	}
	e := newDataEnv()
	r := e.open(t)
	enable := locales.SettingsFullBackupEnable()
	first, again := locales.SettingsFullBackupSetPassword(), locales.SettingsFullBackupConfirmPassword()
	waitUntil(r, "启用表单出现", func() bool { return r.has(gfx.RoleButton, enable) })

	for _, c := range cases {
		r.fill(first, c.pw)
		r.fill(again, c.pw)
		if got := !r.disabled(gfx.RoleButton, enable); got != c.ok {
			t.Errorf("%s：启用钮可用=%v，应为 %v", c.name, got, c.ok)
		}
	}
	if len(e.backup.CallsOf("InitRepository")) != 0 {
		t.Error("只是校验口令，不应调用服务")
	}
}
