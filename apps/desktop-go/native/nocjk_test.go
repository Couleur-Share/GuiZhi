package native

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
// 也没人能用「键对等」检查发现它。本测试用 AST 找出 native/ 下的中日韩字符串字面量。
//
// 放行范围（都不是给用户读的标签）：
//   - 注释（不是字面量，AST 里不出现）；
//   - fmt.Errorf / errors.New / log.* / panic 的参数：诊断信息，仓库约定日志与错误用中文（见 AGENTS.md）；
//   - 生成文件（*_gen.go）与测试文件（*_test.go）。
// 要给用户看的错误原因，应当在界面层用 locales 的文案包一层，再把诊断信息放进可复制的详情。
//
// 例外只有 cjkLiteralExemptFiles 里列的整份文件：写进数据的存储格式标记（不是界面文案，又不能翻译）。

// cjkLiteralExemptFiles 是整份文件放行的例外（相对 native/、用斜杠分隔），值是放行的理由。
// 只给「写进条目数据的存储格式标记」这类不是界面文案、又不能随界面语言翻译的字符串，并且要求集中在一份文件里，
// 这样同一目录下其他文件出现中文字面量仍然会被拦。每加一项都要在 viva-parity-ledger.md 的变更记录里说明。
var cjkLiteralExemptFiles = map[string]string{
	"notes/markers.go": "笔记正文的存储格式标记：小节标题、元数据块字段名与认它们的匹配式（与 packages/shared 的 TS 一致），与界面语言无关",
}

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

// TestNoCJKLiteralsInNativeUI 扫描 native/ 下全部手写的非测试 Go 文件。
func TestNoCJKLiteralsInNativeUI(t *testing.T) {
	scanned := 0
	var violations []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() || !strings.HasSuffix(name, ".go") ||
			strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, "_gen.go") {
			return nil
		}
		scanned++
		if _, exempt := cjkLiteralExemptFiles[filepath.ToSlash(path)]; exempt {
			return nil
		}
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
	// 防止目录挪动后测试空转：骨架里至少有 app、kit、screens、services 的十几个文件
	if scanned < 10 {
		t.Fatalf("只扫描到 %d 个文件，目录结构变了？", scanned)
	}
	for _, v := range violations {
		t.Errorf("界面文案不能写在 Go 代码里，请放进 i18n 目录（native/i18n-src 或渲染层目录）再用 locales 的函数：%s", v)
	}
}

// TestCJKExemptFilesStayNarrow 守住放行名单：每一项必须写明理由，文件必须存在且真的含中日韩字面量
// （否则是过期的放行，删掉）；名单本身只允许是 notes/markers.go 这一类集中存放的标记文件。
func TestCJKExemptFilesStayNarrow(t *testing.T) {
	for path, reason := range cjkLiteralExemptFiles {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("%s：放行必须写明理由", path)
		}
		if strings.Contains(path, "..") || strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") || !strings.HasSuffix(path, ".go") {
			t.Errorf("%s：放行项必须是相对 native/ 的单个 .go 文件", path)
			continue
		}
		v, err := cjkLiteralViolations(filepath.FromSlash(path), nil)
		if err != nil {
			t.Errorf("%s：读取失败（文件挪走了？）：%v", path, err)
			continue
		}
		if len(v) == 0 {
			t.Errorf("%s：文件里已经没有中日韩字面量，这条放行过期了，请从 cjkLiteralExemptFiles 删掉", path)
		}
	}
	if len(cjkLiteralExemptFiles) != 1 {
		t.Errorf("放行名单现有 %d 项；新增整份文件放行要先在台账里说明理由，再同步改这里的期望", len(cjkLiteralExemptFiles))
	}
}
