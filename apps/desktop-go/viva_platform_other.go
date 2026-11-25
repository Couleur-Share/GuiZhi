//go:build !windows

package main

import vapp "github.com/vivaui/viva/app"

// openPathNative 非 Windows：外壳只面向 Windows，这里只为通过编译；沿用 Viva 的 OpenPath（会拒绝目录与可执行文件）。
func openPathNative(p string) error { return vapp.OpenPath(p) }
