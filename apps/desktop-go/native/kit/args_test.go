package kit

import (
	"strings"
	"testing"
)

func TestParseHue(t *testing.T) {
	h, s, err := ParseHue("teal")
	if err != nil || h != 175 || s != 30 {
		t.Fatalf("预设 teal = (%v, %v, %v)，期望 (175, 30, nil)", h, s, err)
	}
	for _, in := range []string{"150,30", " 150 , 30 ", "0,0", "360,100", "12.5,7.5"} {
		if _, _, err := ParseHue(in); err != nil {
			t.Errorf("ParseHue(%q) 不应报错：%v", in, err)
		}
	}
	h, s, _ = ParseHue("150,30")
	if h != 150 || s != 30 {
		t.Errorf("自定义色 = (%v, %v)", h, s)
	}
	for _, in := range []string{"", "nope", "361,10", "10,101", "-1,10", "a,b", "10", "10,20,30"} {
		_, _, err := ParseHue(in)
		if err == nil {
			t.Errorf("ParseHue(%q) 应报错", in)
			continue
		}
		// 错误里要列出可选预设，写错的人才知道怎么改
		if !strings.Contains(err.Error(), "royal-blue") {
			t.Errorf("ParseHue(%q) 的错误应列出可选预设：%v", in, err)
		}
	}
}

func TestParseEnumerations(t *testing.T) {
	for _, in := range []string{"system", "light", "dark"} {
		if m, err := ParseMode(in); err != nil || string(m) != in {
			t.Errorf("ParseMode(%q) = %q, %v", in, m, err)
		}
	}
	if _, err := ParseMode("auto"); err == nil || !strings.Contains(err.Error(), "dark") {
		t.Errorf("ParseMode(auto) 应报错并列出可选项：%v", err)
	}
	for _, in := range []string{"system", "off", "reduced", "standard"} {
		if m, err := ParseMotion(in); err != nil || string(m) != in {
			t.Errorf("ParseMotion(%q) = %q, %v", in, m, err)
		}
	}
	if _, err := ParseMotion("fast"); err == nil {
		t.Error("ParseMotion(fast) 应报错")
	}
	for _, f := range FontSizes {
		if id, err := ParseFontSize(f.ID); err != nil || id != f.ID {
			t.Errorf("ParseFontSize(%q) = %q, %v", f.ID, id, err)
		}
	}
	if _, err := ParseFontSize("huge"); err == nil || !strings.Contains(err.Error(), "medium") {
		t.Errorf("ParseFontSize(huge) 应报错并列出可选项：%v", err)
	}
}
