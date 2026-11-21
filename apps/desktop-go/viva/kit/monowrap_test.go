package kit

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// 原文与折行宽度取自 React 参照的 ui.toast.detail-expanded：字号 11px，pre 的内容宽 334px，
// 参照里量到的六行（行矩形的宽度除以 6.0479 得到字符数：31 / 2+29 / 2+28 / 26+1 / 55 / 26）。
func TestWrapMonoMatchesChromiumFixture(t *testing.T) {
	raw, err := os.ReadFile("../parity/testdata/toast-content.json")
	if err != nil {
		t.Fatal(err)
	}
	var content struct {
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(raw, &content); err != nil {
		t.Fatal(err)
	}
	got := WrapMono(content.Detail, 334, 11)
	want := []string{
		"SQLITE_BUSY: database is locked",
		"  at updateItems (items.go:214)",
		"  at batchUpdate (batch.go:88)",
		"failed to update 12 items: ",
		"C:/Users/someone/AppData/Roaming/GuiZhi/data/knowledge.",
		"db: database is locked (5)",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("折行与 Chromium 参照不一致：\n got: %q\nwant: %q", got, want)
	}
}

func TestWrapMonoRules(t *testing.T) {
	const size = 10 // 字距 5.498，宽字符 10
	cases := []struct {
		name  string
		text  string
		width float32
		want  []string
	}{
		{"空文字是一行空串", "", 100, []string{""}},
		{"硬换行保留，空行也保留", "ab\n\ncd", 100, []string{"ab", "", "cd"}},
		{"CRLF 当作一个换行", "ab\r\ncd", 100, []string{"ab", "cd"}},
		{"宽度不大于零只按硬换行切", "abc def\nghi", 0, []string{"abc def", "ghi"}},
		{"在空格之后换行，行尾空格悬挂", "aaaa bbbb cccc", 5.498*8 + 0.1, []string{"aaaa ", "bbbb ", "cccc"}},
		{"恰好放下时不换行（行尾空格不计宽度）", "aaaa bbbb cccc", 5.498*9 + 0.1, []string{"aaaa bbbb ", "cccc"}},
		{"前面有机会时整词移到下一行，不在词里断", "ab cdefgh", 5.498*6 + 0.1, []string{"ab ", "cdefgh"}},
		{"空行也放不下的长词在任意字符处断开", "abcdefghij", 5.498*4 + 0.1, []string{"abcd", "efgh", "ij"}},
		{"斜杠之后没有断行机会", "x /aa/bb/cc", 5.498*8 + 0.1, []string{"x ", "/aa/bb/c", "c"}},
		{"连字符之后是机会，数字之前不是", "foo-bar 1-2", 5.498*5 + 0.1, []string{"foo-", "bar ", "1-2"}},
		{"汉字之间随处可断", "汉字汉字汉字", 10*4 + 0.1, []string{"汉字汉字", "汉字"}},
		{"避头：关闭类标点不起行", "汉字汉，字", 10*2 + 0.1, []string{"汉字", "汉，", "字"}},
		{"制表符展开到下一个 8 的倍数列", "a\tb", 100, []string{"a       b"}},
		{"行首缩进保留", "  ab", 100, []string{"  ab"}},
		{"只有空格的行保留", "   ", 100, []string{"   "}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := WrapMono(tc.text, tc.width, size)
			if strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
				t.Fatalf("WrapMono(%q, %v)\n got: %q\nwant: %q", tc.text, tc.width, got, tc.want)
			}
		})
	}
}

func TestMonoWidthCountsWideRunesAsOneEm(t *testing.T) {
	if got, want := MonoWidth("ab汉", 10), 2*10*consolasAdvance+10; got < float32(want)-1e-3 || got > float32(want)+1e-3 {
		t.Fatalf("MonoWidth = %v，期望 %v", got, want)
	}
	// 大字号档 13.5px 下新宋体位图吸附到 14px，13 个汉字占 182px、14 个占 196px（B-072）。
	if got, want := MonoWidth("汉", 13.5), float32(14); got != want {
		t.Fatalf("MonoWidth(13.5) = %v，期望 %v", got, want)
	}
	if got, want := MonoWidth("汉", 10.5), float32(10.5); got != want {
		t.Fatalf("MonoWidth(10.5) = %v，期望 %v", got, want)
	}
	// 栏宽 190px、字号 13.5px 时 14 个汉字（196px）放不下，应折成 13 + 1（而不是按 13.5px 算出的 14）。
	got := WrapMono("本地优先的软件把数据放在用户", 190, 13.5)
	if len(got) != 2 || len([]rune(got[0])) != 13 || len([]rune(got[1])) != 1 {
		t.Fatalf("13.5px 下 190px 宽折行 = %q，期望 13+1 字", got)
	}
}
