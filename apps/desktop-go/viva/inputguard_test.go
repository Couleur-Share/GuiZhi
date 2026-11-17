package viva

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// 文字输入的统一封装守卫（台账 V-a：中文输入法真机测试推迟，业务界面必须能「整体替换实现」）。
//
// 所有文字输入面——单行输入、多行文本框、代码编辑器——都经 viva/kit 的 Look.Input 一类封装；业务界面
// （viva/screens、viva/app、viva/island 等）不得直接调用 Viva 的输入控件。这样一旦 V-a 在真机上不通过，
// 只改 kit 里的实现，不用逐屏返工。本测试用 AST 找出 viva/kit 之外对 Viva 输入控件的直接引用。
//
// 放行范围：
//   - viva/kit 自己（封装就在那里）；
//   - 状态类型 ui.Field（只是装文字的容器，经 kit.Input 传入；它本身不产生控件）；
//   - 测试文件（*_test.go）与生成文件（*_gen.go）。
//
// 引用靠 import 路径识别，换了本地别名（import u "github.com/vivaui/viva/ui"）也逃不掉。

const (
	pathVivaUI         = "github.com/vivaui/viva/ui"
	pathVivaComponents = "github.com/vivaui/viva/components"
	pathVivaApp        = "github.com/vivaui/viva/app"
)

// forbiddenInputPrefixes 是各个 Viva 包里「文字输入控件」导出名的前缀；按包登记，名字匹配前缀即视为直接使用。
//
// ui 包里的 Field 是状态类型，不在其中；其余带 Field / TextField 的函数（TextFieldBase、TextFieldID……）、
// Textarea、Input、InputGroup、Combobox、CodeEditor，以及把文字框接入输入法的 Fields（TextInputProvider）都是控件。
var forbiddenInputPrefixes = map[string][]string{
	pathVivaUI:         {"TextField", "Textarea", "TextArea", "Input", "Combobox", "CodeEditor", "Fields", "FieldBase", "FieldArea"},
	pathVivaComponents: {"Input", "Textarea", "TextArea", "CodeEditor", "Combobox"},
	pathVivaApp:        {"TextInput"},
}

// inputControlViolations 解析 src，返回其中对 Viva 输入控件的直接引用（"文件:行: 包.名字"）。
func inputControlViolations(filename string, src any) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	// 本文件里每个相关包的本地名字 → import 路径。
	local := map[string]string{}
	for _, imp := range file.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return nil, err
		}
		if _, watched := forbiddenInputPrefixes[p]; !watched {
			continue
		}
		name := path.Base(p)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		if name != "_" && name != "." {
			local[name] = p
		}
	}
	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		importPath, ok := local[pkg.Name]
		if !ok {
			return true
		}
		for _, prefix := range forbiddenInputPrefixes[importPath] {
			if strings.HasPrefix(sel.Sel.Name, prefix) {
				p := fset.Position(sel.Pos())
				out = append(out, fmt.Sprintf("%s:%d: %s.%s", p.Filename, p.Line, pkg.Name, sel.Sel.Name))
				break
			}
		}
		return true
	})
	return out, nil
}

func TestInputGuardChecker(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"直接调用 ui 的文本框", `package p
import "github.com/vivaui/viva/ui"
func f(c ui.C, field *ui.Field) { ui.TextFieldBase(c, field, "名称", "") }`, 1},
		{"别名导入也要抓", `package p
import u "github.com/vivaui/viva/ui"
func f(c u.C, field *u.Field) { u.TextFieldID(c, 1, field, "名称", "") }`, 1},
		{"ui.Field 状态类型放行", `package p
import "github.com/vivaui/viva/ui"
type s struct{ search ui.Field }`, 0},
		{"components 的输入控件要抓", `package p
import "github.com/vivaui/viva/components"
var _ = components.InputSnapshot{}
var _ components.CodeEditor`, 2},
		{"components 的非输入控件放行", `package p
import "github.com/vivaui/viva/components"
var _ components.Dialog`, 0},
		{"app 的输入法接入要抓", `package p
import "github.com/vivaui/viva/app"
var _ app.TextInputProvider`, 1},
		{"ui.Fields（输入法提供者）要抓", `package p
import "github.com/vivaui/viva/ui"
func f(a, b *ui.Field) { _ = ui.Fields(a, b) }`, 1},
		{"滑块、按钮等非文字输入放行", `package p
import "github.com/vivaui/viva/ui"
func f(c ui.C, s *ui.SliderState) { ui.Slider(c, s); ui.SliderBase(c, s) }`, 0},
		{"调用 kit 的封装放行", `package p
import "guizhi-mygo-shell/viva/kit"
func f(l *kit.Look, c any, field any) { l.Input(c, field, "名称", "", kit.InputSpec{}) }`, 0},
		{"与包同名的局部变量不误报（没有导入该包）", `package p
func f(ui interface{ Input() }) { ui.Input() }`, 0},
		{"多处引用逐处报告", `package p
import "github.com/vivaui/viva/ui"
func f(c ui.C, a, b *ui.Field) { ui.TextFieldBase(c, a, "", ""); ui.Textarea(nil); ui.CodeEditor(nil) }`, 3},
	}
	for _, c := range cases {
		got, err := inputControlViolations("case.go", c.src)
		if err != nil {
			t.Fatalf("%s：解析失败：%v", c.name, err)
		}
		if len(got) != c.want {
			t.Errorf("%s：期望 %d 处，实际 %d：%v", c.name, c.want, len(got), got)
		}
	}
}

// TestInputGuardCanary 守住守卫本身没有过期：封装 viva/kit/input.go 必须能被这个检查器「抓到」。
// 如果 Viva 升级后输入控件改了名字，kit 里的调用不再命中前缀表，这里先失败，提醒更新 forbiddenInputPrefixes，
// 否则业务界面直接用新名字的输入控件时守卫会悄悄放过。
func TestInputGuardCanary(t *testing.T) {
	got, err := inputControlViolations(filepath.Join("kit", "input.go"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("kit/input.go 没有命中任何输入控件前缀：Viva 的输入控件改名了？请更新 forbiddenInputPrefixes")
	}
}

// TestNoDirectInputControlsOutsideKit 扫描 viva/ 下除 kit 之外全部手写的非测试 Go 文件。
func TestNoDirectInputControlsOutsideKit(t *testing.T) {
	scanned := 0
	var violations []string
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() && filepath.ToSlash(p) == "kit" {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(name, ".go") ||
			strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, "_gen.go") {
			return nil
		}
		scanned++
		v, err := inputControlViolations(p, nil)
		if err != nil {
			return err
		}
		violations = append(violations, v...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// 防止目录挪动后测试空转：屏幕、应用外壳、岛、对照工具合起来远多于这个数。
	if scanned < 20 {
		t.Fatalf("只扫描到 %d 个文件，目录结构变了？", scanned)
	}
	for _, v := range violations {
		t.Errorf("业务界面不得直接使用 Viva 的文字输入控件，请经 viva/kit 的封装（Look.Input 等，V-a 失败时整体替换实现）：%s", v)
	}
}
