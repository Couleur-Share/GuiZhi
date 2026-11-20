package viva

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// 阴影令牌守卫（台账 G-030）。
//
// 渲染层的 Tailwind 类 shadow-sm、shadow-lg 不是 globals.css 里的 --shadow-sm、--shadow-lg 令牌：tailwind.config.js 没有覆盖
// boxShadow，它们是 Tailwind 的默认值（shadow-sm = 0 1px 2px 0 黑 5%，shadow-lg = 两层带负扩散的黑 10%），明暗主题相同；
// 而令牌在深色下是黑 40% / 60%（浅色是 4% / 8%）。用令牌去画这两个类，深色下会多出一圈明显的暗晕——整窗 L3 的逐通道容差与整窗比例
// 看不见它（M2-4 收口时由低幅度审计发现并改掉：rail 的模块钮 / 角标 / 设置钮、设置页导航的选中项、导入行内动作、详情查找条与
// 专注阅读的浮钮，共 7 处调用）。
// Tailwind 默认阴影在 viva/kit/shadows.go（kit.ShadowSM、kit.ShadowLG 等），用 Surface.Shadows / ButtonStyle.Shadows 传入。
//
// 本测试用 AST 找出 viva/tokens 之外对令牌字段 ShadowSm、ShadowLg 的直接引用。引用 var(--shadow-*) 的 CSS 变量（--app-ui-*-shadow
// 等，生成成 UiPanelShadow 一类字段）不在此列。真有渲染层直接写 `shadow-[var(--shadow-lg)]` 的场合再加放行名单，并写明依据。

// tokenShadowFields 是主题令牌里与 Tailwind 阴影类重名的字段。
var tokenShadowFields = map[string]bool{"ShadowSm": true, "ShadowLg": true}

// tokenShadowViolations 解析 src，返回其中对令牌阴影字段的直接引用（"文件:行: 表达式"）。
func tokenShadowViolations(filename string, src any) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || !tokenShadowFields[sel.Sel.Name] {
			return true
		}
		p := fset.Position(sel.Pos())
		out = append(out, fmt.Sprintf("%s:%d: .%s", p.Filename, p.Line, sel.Sel.Name))
		return true
	})
	return out, nil
}

func TestTokenShadowGuardChecker(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"用令牌画 shadow-sm 要抓", `package p
func f(t theme, s *surface) { s.Shadow = t.ShadowSm }`, 1},
		{"用令牌画 shadow-lg 要抓", `package p
func f(t theme) any { return surface{Shadow: t.ShadowLg} }`, 1},
		{"两处各报各的", `package p
func f(t theme) { _ = t.ShadowSm; _ = t.ShadowLg }`, 2},
		{"kit 里的 Tailwind 默认阴影放行（大小写不同的另一个名字）", `package p
func f(s *surface) { s.Shadows = kit.ShadowSM; s.Shadows = kit.ShadowLG }`, 0},
		{"CSS 变量生成的字段放行", `package p
func f(t theme, s *surface) { s.Shadow = t.UiPanelShadow }`, 0},
		{"同名的局部变量不是对令牌字段的引用", `package p
func f() { ShadowSm := 1; _ = ShadowSm }`, 0},
	}
	for _, c := range cases {
		got, err := tokenShadowViolations("case.go", c.src)
		if err != nil {
			t.Fatalf("%s：解析失败：%v", c.name, err)
		}
		if len(got) != c.want {
			t.Errorf("%s：期望 %d 处，实际 %d：%v", c.name, c.want, len(got), got)
		}
	}
}

// TestTokenShadowGuardCanary 守住守卫本身没有过期：生成的令牌结构里必须还有被守的字段名。
// 令牌改名之后业务界面直接用新名字时，守卫会悄悄放过，所以这里先失败，提醒更新 tokenShadowFields。
func TestTokenShadowGuardCanary(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), filepath.Join("tokens", "tokens_gen.go"), nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		if f, ok := n.(*ast.Field); ok {
			for _, name := range f.Names {
				declared[name.Name] = true
			}
		}
		return true
	})
	for name := range tokenShadowFields {
		if !declared[name] {
			t.Errorf("tokens_gen.go 里没有字段 %s：令牌改名了？请更新 tokenShadowFields", name)
		}
	}
}

// TestNoTokenShadowsOutsideTokens 扫描 viva/ 下除 tokens 之外全部手写的非测试 Go 文件。
func TestNoTokenShadowsOutsideTokens(t *testing.T) {
	scanned := 0
	var violations []string
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() && filepath.ToSlash(p) == "tokens" {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(name, ".go") ||
			strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, "_gen.go") {
			return nil
		}
		scanned++
		v, err := tokenShadowViolations(p, nil)
		if err != nil {
			return err
		}
		violations = append(violations, v...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// 防止目录挪动后测试空转：屏幕、应用外壳、套件、对照工具合起来远多于这个数。
	if scanned < 20 {
		t.Fatalf("只扫描到 %d 个文件，目录结构变了？", scanned)
	}
	for _, v := range violations {
		t.Errorf("Tailwind 的 shadow-sm / shadow-lg 不是 --shadow-sm / --shadow-lg 令牌，请用 viva/kit/shadows.go 的 kit.ShadowSM / kit.ShadowLG（台账 G-030）：%s", v)
	}
}
