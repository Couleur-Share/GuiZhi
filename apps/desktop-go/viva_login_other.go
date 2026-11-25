//go:build !windows

package main

import (
	"errors"
	"os"
)

// 非 Windows：外壳只面向 Windows，这里只为通过编译。
func osCommandLineArgs() []string { return append([]string(nil), os.Args[1:]...) }

func setVivaLoginItem(bool) error { return errors.New("开机自启只支持 Windows") }
