//go:build !windows

package main

import (
	"fmt"
	"os"
)

// showStartupErrorDialog 非 Windows 无统一的原生对话框，输出到 stderr（官方只支持 Windows）。
func showStartupErrorDialog(title, text string) {
	fmt.Fprintf(os.Stderr, "%s\n%s\n", title, text)
}
