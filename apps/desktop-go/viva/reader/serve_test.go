package reader

import (
	"strings"
	"testing"
)

func TestIndexHTMLKeepsTheTagThatBakingReplaces(t *testing.T) {
	// 构建产物里 <html> 标签的写法一变，bakeHTML 就静默失效（主题不再烘进去）：这里把它钉住。
	raw, err := assets.ReadFile("assets/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), htmlTag) != 1 {
		t.Fatalf("assets/index.html 里要恰有一处 %s：\n%s", htmlTag, raw)
	}
	for _, name := range []string{"assets/reader.js", "assets/reader.css"} {
		if data, err := assets.ReadFile(name); err != nil || len(data) == 0 {
			t.Errorf("%s 缺失或为空：%v", name, err)
		}
	}
}

func TestBakeHTML(t *testing.T) {
	raw := []byte("<!DOCTYPE html>\n" + htmlTag + "\n<head></head></html>")
	cases := []struct {
		name  string
		theme Theme
		lang  Lang
		want  string
	}{
		{"浅色中文", Theme{Hue: 210, Saturation: 35, BasePx: 16, Motion: MotionStandard}, LangZh,
			`<html lang="zh-CN" style="--theme-hue:210;--theme-saturation:35;--base-font-size:16px" data-motion="standard">`},
		{"深色英文", Theme{Dark: true, Hue: 142.5, Saturation: 20, BasePx: 18, Motion: MotionReduced}, LangEn,
			`<html lang="en" class="dark" style="--theme-hue:142.5;--theme-saturation:20;--base-font-size:18px" data-motion="reduced">`},
		{"未知语言按中文", Theme{Hue: 0, Saturation: 0, BasePx: 14, Motion: MotionOff}, "fr",
			`<html lang="zh-CN" style="--theme-hue:0;--theme-saturation:0;--base-font-size:14px" data-motion="off">`},
		{"无效外观退回默认", Theme{BasePx: 1}, LangZh,
			`<html lang="zh-CN" style="--theme-hue:210;--theme-saturation:35;--base-font-size:16px" data-motion="standard">`},
	}
	for _, c := range cases {
		got := string(bakeHTML(raw, c.theme, c.lang))
		if !strings.Contains(got, c.want) {
			t.Errorf("%s：\n  实际 %s\n  期望含 %s", c.name, got, c.want)
		}
		if strings.Contains(got, htmlTag) || strings.Count(got, "<html") != 1 {
			t.Errorf("%s：原标签要被替换且只有一个 <html>：%s", c.name, got)
		}
		if !strings.HasPrefix(got, "<!DOCTYPE html>\n") || !strings.HasSuffix(got, "\n<head></head></html>") {
			t.Errorf("%s：标签之外的内容不能动：%q", c.name, got)
		}
	}
	// 没有预期标签：原样返回，不报错也不乱插。
	if got := string(bakeHTML([]byte("<html>"), lightTheme, LangZh)); got != "<html>" {
		t.Errorf("没有预期标签应当原样返回：%q", got)
	}
}

func TestBakedValuesMatchWhatThePageWritesLater(t *testing.T) {
	// 页面脚本的 applyTheme 用 String(number) 写同样的值：整数不带小数点，小数取最短表示。
	for v, want := range map[float64]string{0: "0", 16: "16", 210: "210", 142.5: "142.5", 0.1: "0.1", 359.99: "359.99", 18.0: "18"} {
		if got := formatNumber(v); got != want {
			t.Errorf("formatNumber(%v) = %q，期望 %q", v, got, want)
		}
	}
}

func TestServeIndexUsesTheLatestRequestTheme(t *testing.T) {
	r := newRig(t)
	r.req.Theme.Dark = true
	r.req.Lang = LangEn
	r.frame()
	mime, data, ok := r.host.serve("index.html")
	if !ok || mime != "text/html; charset=utf-8" {
		t.Fatalf("serve(index.html) = %q %v", mime, ok)
	}
	if html := string(data); !strings.Contains(html, `class="dark"`) || !strings.Contains(html, `lang="en"`) {
		t.Errorf("没有烘进当前外观：%s", html)
	}
	for _, p := range []string{"", "/", "/index.html"} {
		if _, d, ok := r.host.serve(p); !ok || !strings.Contains(string(d), `class="dark"`) {
			t.Errorf("serve(%q) 也该给出烘好的首页：ok=%v", p, ok)
		}
	}
}

func TestServeAssets(t *testing.T) {
	h := New(Options{})
	for path, wantMIME := range map[string]string{
		"reader.js":  "text/javascript; charset=utf-8",
		"reader.css": "text/css; charset=utf-8",
		"/reader.js": "text/javascript; charset=utf-8",
	} {
		mime, data, ok := h.serve(path)
		if !ok || mime != wantMIME || len(data) == 0 {
			t.Errorf("serve(%q) = %q, %d 字节, %v", path, mime, len(data), ok)
		}
	}
	for _, path := range []string{
		"nope.js", "../reader.js", "a/../reader.js", "./reader.js", "reader.js/", `..\reader.js`, "C:/Windows/win.ini", "reader.js\x00.png",
		".git", "assets", "assets/reader.js", "//reader.js", "reader.js?x=1",
	} {
		if _, _, ok := h.serve(path); ok {
			t.Errorf("serve(%q) 应当 404", path)
		}
	}
}

func TestAssetMIME(t *testing.T) {
	for name, want := range map[string]string{
		"a.js": "text/javascript; charset=utf-8", "a.MJS": "text/javascript; charset=utf-8", "a.css": "text/css; charset=utf-8",
		"a.html": "text/html; charset=utf-8", "a.json": "application/json; charset=utf-8", "a.svg": "image/svg+xml",
		"a.woff2": "font/woff2", "a.woff": "font/woff", "a.png": "image/png", "a.bin": "application/octet-stream", "noext": "application/octet-stream",
	} {
		if got := assetMIME(name); got != want {
			t.Errorf("assetMIME(%q) = %q，期望 %q", name, got, want)
		}
	}
}

func TestServeMedia(t *testing.T) {
	var asked []string
	h := New(Options{Media: func(scheme, name string) (string, []byte, bool) {
		asked = append(asked, scheme+"|"+name)
		if name == "missing.png" {
			return "", nil, false
		}
		return "image/png", []byte("PNG"), true
	}})
	mime, data, ok := h.serve("media/local-image/图 1.png")
	if !ok || mime != "image/png" || string(data) != "PNG" {
		t.Fatalf("serve(media) = %q %q %v", mime, data, ok)
	}
	if _, _, ok := h.serve("media/local-video/clip.mp4"); !ok {
		t.Error("local-video 也要放行")
	}
	if _, _, ok := h.serve("media/local-image/sub/dir/a.png"); !ok || asked[len(asked)-1] != "local-image|sub/dir/a.png" {
		t.Errorf("名称保留子路径交给 Media 校验：%v", asked)
	}
	if _, _, ok := h.serve("media/local-image/missing.png"); ok {
		t.Error("Media 说没有就是 404")
	}
	n := len(asked)
	for _, path := range []string{"media/", "media/local-image", "media/local-image/", "media/http/a.png", "media/file/a.png", "media/Local-Image/a.png", "media//a.png"} {
		if _, _, ok := h.serve(path); ok {
			t.Errorf("serve(%q) 应当 404", path)
		}
	}
	if len(asked) != n {
		t.Errorf("不合规的路径不能走到 Media：%v", asked[n:])
	}
	if _, _, ok := New(Options{}).serve("media/local-image/a.png"); ok {
		t.Error("没有 Media 时媒体一律 404")
	}
}
