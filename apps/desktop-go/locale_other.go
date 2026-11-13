//go:build !windows

package main

import (
	"os"
	"strings"
)

// systemLocale 在非 Windows 上取 LANG（如 "zh_CN.UTF-8" → "zh-CN"）；官方只支持 Windows，这里仅保证可编译。
func systemLocale() string {
	for _, k := range []string{"LC_ALL", "LANG"} {
		if v := os.Getenv(k); v != "" {
			if i := strings.IndexAny(v, ".@"); i >= 0 {
				v = v[:i]
			}
			return strings.ReplaceAll(v, "_", "-")
		}
	}
	return "en"
}
