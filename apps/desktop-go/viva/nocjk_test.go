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
	"unicode"
)

// 界面文案必须经 locales 目录（i18n），不能直接写在 Go 代码里，否则切到英文时会漏翻、
// 也没人能用「键对等」检查发现它。本测试用 AST 找出 viva/ 下的中日韩字符串字面量（native/ 的同名规则扩到 Viva 路线）。
//
// 放行范围（都不是给用户读的标签）：
//   - 注释（不是字面量，AST 里不出现）；
//   - fmt.Errorf / errors.New / log.* / panic 的参数：诊断信息，仓库约定日志与错误用中文（见 AGENTS.md）；
//   - 生成文件（*_gen.go）与测试文件（*_test.go）。
// 要给用户看的错误原因，应当在界面层用 locales 的文案包一层，再把诊断信息放进可复制的详情。

// isCJKRune 判断是否中日韩文字或全角标点。
func isCJKRune(r rune) bool {
	if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) {
		return true
	}
	return (r >= 0x3000 && r <= 0x303F) || (r >= 0xFF00 && r <= 0xFFEF) // CJK 符号标点、全角形式
}

func hasCJK(s string) bool {
	for _, r := range s {
		if isCJKRune(r) {
			return true
		}
	}
	return false
}

// isDiagnosticCall 报告 call 是否是诊断类调用（其参数里的字面量放行）。
func isDiagnosticCall(call *ast.CallExpr) bool {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name == "panic"
	case *ast.SelectorExpr:
		pkg, ok := fn.X.(*ast.Ident)
		if !ok {
			return false
		}
		switch pkg.Name + "." + fn.Sel.Name {
		case "fmt.Errorf", "errors.New",
			"log.Print", "log.Printf", "log.Println", "log.Fatal", "log.Fatalf", "log.Fatalln":
			return true
		}
	}
	return false
}

// cjkLiteralViolations 解析 src，返回其中不该出现的中日韩字面量（"文件:行: 字面量"）。
func cjkLiteralViolations(filename string, src any) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	allowed := map[token.Pos]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !isDiagnosticCall(call) {
			return true
		}
		for _, arg := range call.Args {
			ast.Inspect(arg, func(m ast.Node) bool {
				if lit, ok := m.(*ast.BasicLit); ok {
					allowed[lit.Pos()] = true
				}
				return true
			})
		}
		return true
	})
	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || (lit.Kind != token.STRING && lit.Kind != token.CHAR) || allowed[lit.Pos()] || !hasCJK(lit.Value) {
			return true
		}
		p := fset.Position(lit.Pos())
		out = append(out, fmt.Sprintf("%s:%d: %s", p.Filename, p.Line, lit.Value))
		return true
	})
	return out, nil
}

func TestCJKLiteralChecker(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"界面文案字面量必须报", `package p
func f(c ctx) { c.Text("采集任务") }`, 1},
		{"Sprintf 拼出来的界面文案也要报", `package p
func f() string { return fmt.Sprintf("共 %d 条，进行中 %d", 1, 2) }`, 1},
		{"全角标点也要报", `package p
var sep = "、"`, 1},
		{"字符字面量要报", `package p
var r = '、'`, 1},
		{"原始字符串要报", "package p\nvar s = `任务`", 1},
		{"注释放行", `package p
// 这是注释
func f() {} // 行尾注释`, 0},
		{"Errorf 参数放行（含嵌套调用里的字面量）", `package p
func f(ids []string) error { return fmt.Errorf("未知的值（可选：%s）", strings.Join(ids, "、")) }`, 0},
		{"errors.New 与 log 放行", `package p
func f() error { log.Printf("[x] 失败：%v", 1); return errors.New("失败") }`, 0},
		{"panic 放行", `package p
func f() { panic("不可达") }`, 0},
		{"纯英文与标识符放行", `package p
const k = "hello"
func f(c ctx) { c.Text(k) }`, 0},
		{"日文和韩文也算", `package p
var a, b = "こんにちは", "안녕"`, 2},
	}
	for _, c := range cases {
		got, err := cjkLiteralViolations("case.go", c.src)
		if err != nil {
			t.Fatalf("%s：解析失败：%v", c.name, err)
		}
		if len(got) != c.want {
			t.Errorf("%s：期望 %d 处，实际 %d：%v", c.name, c.want, len(got), got)
		}
	}
}

// devToolDirs 是开发期工具目录：输出给开发者看（命令行帮助、对照报告），不是用户界面，不受界面文案规则约束。
var devToolDirs = map[string]bool{
	"icons/gen": true, // SVG → gfx.Icon 的生成器命令
	"parity":    true, // 渲染层对照工具与报告
}

// TestNoCJKLiteralsInVivaUI 扫描 viva/ 下全部手写的非测试 Go 文件。
func TestNoCJKLiteralsInVivaUI(t *testing.T) {
	scanned := 0
	var violations []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() && devToolDirs[filepath.ToSlash(path)] {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(name, ".go") ||
			strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, "_gen.go") {
			return nil
		}
		scanned++
		v, err := cjkLiteralViolations(path, nil)
		if err != nil {
			return err
		}
		violations = append(violations, v...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// 防止目录挪动后测试空转：骨架里至少有 app、island、kit、tokens、locales 的若干文件
	if scanned < 8 {
		t.Fatalf("只扫描到 %d 个文件，目录结构变了？", scanned)
	}
	for _, v := range violations {
		t.Errorf("界面文案不能写在 Go 代码里，请放进 i18n 目录（native/i18n-src 或渲染层目录）再用 viva/locales 的函数：%s", v)
	}
}
