package ask

import (
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/viva/locales"
)

// 发送钮的禁用态按「去掉首尾空白后是否为空」判断（JS 的 trim 口径，含 NBSP 与全角空格），不是只看有没有字符：
// 点击只会被 submit 的同一条守卫挡住，所以得直接读钮的可访问状态才能看出禁用态对不对。
func TestUISendButtonDisabledStateFollowsTheTrimmedDraft(t *testing.T) {
	r := newUIRig(t)
	disabled := func() bool {
		t.Helper()
		n, ok := r.node(gfx.RoleButton, locales.AskSend())
		if !ok {
			t.Fatalf("找不到发送钮；画面上有 %q", r.session.Texts())
		}
		return n.Disabled
	}
	for _, blank := range []string{"", " ", "   ", " \n\t ", "\u00a0\u3000"} {
		r.screen.setDraft(blank)
		r.frames(2)
		if !disabled() {
			t.Errorf("草稿 %q 去掉空白后为空，发送钮应禁用", blank)
		}
	}
	for _, text := range []string{"x", "  问题  "} {
		r.screen.setDraft(text)
		r.frames(2)
		if disabled() {
			t.Errorf("草稿 %q 有内容，发送钮应可用", text)
		}
	}
}
