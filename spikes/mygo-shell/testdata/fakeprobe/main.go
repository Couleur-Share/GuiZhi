// 测试替身：按参数打印 ffmpeg / yt-dlp 的版本串；FAKE_TOOL_FAIL=1 时以非零码退出。
package main

import (
	"fmt"
	"os"
)

func main() {
	if os.Getenv("FAKE_TOOL_FAIL") == "1" {
		os.Exit(3)
	}
	for _, a := range os.Args[1:] {
		switch a {
		case "-version":
			fmt.Println("ffmpeg version N-125753-g6095372a70-20260724 Copyright (c) 2000-2026 the FFmpeg developers")
			return
		case "--version":
			fmt.Println("2026.07.15")
			return
		}
	}
	os.Exit(2)
}
