package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/vivaui/viva/gfx"
)

// 窗口接线（bindInput，与 Run 同一处）：滚轮把设置页的内容滚动了，就向窗口请求一次指针移动，位置是滚轮事件的位置（台账 B-031）。
// 800×600 的设置页内容放不下，指针下方是滚动区。
func TestWindowWiringAsksForAHoverRefreshAfterTheWheel(t *testing.T) {
	f := openRootSize(t, 800, 600, nil)
	if err := f.session.Scroll(gfx.ScrollEvent{X: 500, Y: 400, DeltaY: 120, Precise: true}); err != nil {
		t.Fatalf("滚动失败：%v", err)
	}
	if len(f.hoverPosts) != 1 || f.hoverPosts[0] != [2]float32{500, 400} {
		t.Fatalf("滚轮滚动了设置页：请求 %v，期望恰好一次、位置是滚轮事件的位置 (500,400)", f.hoverPosts)
	}
	// 没有任何滚动区接走滚轮（指针在 rail 上）：没有内容换位，不请求。
	f.hoverPosts = nil
	if err := f.session.Scroll(gfx.ScrollEvent{X: 30, Y: 300, DeltaY: 120, Precise: true}); err != nil {
		t.Fatalf("滚动失败：%v", err)
	}
	if len(f.hoverPosts) != 0 {
		t.Fatalf("滚轮落在 rail 上没有滚动区接走，不该请求指针移动，实际 %v", f.hoverPosts)
	}
}

// Run 必须调用 bindInput：Run 开真窗口，测不了，用语法树核对。少了这一行，窗口里的 Tab 可达、提示收起与滚轮之后的悬停刷新会一起失效
// （无头测试走的是同一个 bindInput，不会发现）。
func TestRunBindsWindowInput(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "app.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("解析 app.go 失败：%v", err)
	}
	var run *ast.FuncDecl
	for _, d := range file.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == "Run" {
			run = fn
		}
	}
	if run == nil {
		t.Fatal("app.go 里没有 Run")
	}
	bound := false
	ast.Inspect(run, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "bindInput" {
				bound = true
			}
		}
		return !bound
	})
	if !bound {
		t.Fatal("Run 没有调用 bindInput：窗口的 OnKey 与 OnScroll 钩子没有接上")
	}
}
