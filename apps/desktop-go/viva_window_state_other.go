//go:build !windows

package main

// vivaDisplayWorkAreas 非 Windows：外壳只面向 Windows，这里只为通过编译；没有显示器信息时恢复逻辑回落到默认窗口。
func vivaDisplayWorkAreas() []windowBounds { return nil }
