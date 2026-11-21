//go:build windows

package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
)

// 冒烟用的内容：标题、列表、引用、表格、代码块、外链、本地图（在与不在）与足够长的正文（要出现滚动才能核对阅读位置）。
const (
	// 命中词：正文里每个小节都有，核对查找计数。
	findWord = "并发"
	// 页面里的外链：programmatic click 之后宿主应收到同一个地址。
	linkURL = "https://example.com/a"
)

func markdownFixture() string {
	var b strings.Builder
	b.WriteString("# 并发模式笔记\n\n")
	b.WriteString("Go 的并发建立在 **goroutine** 与 *channel* 之上，这份内容用来核对阅读岛的渲染：并发、English text、标点。\n\n")
	b.WriteString("## 列表与引用\n\n- 第一项\n- 第二项 `inline code`\n\n> 不要通过共享内存来通信，而要通过通信来共享内存。\n\n")
	b.WriteString("## 表格\n\n| 名称 | 说明 |\n| --- | --- |\n| mutex | 互斥锁 |\n| channel | 通道 |\n\n")
	b.WriteString("## 代码\n\n```go\nfunc main() {\n\tch := make(chan int)\n\tgo func() { ch <- 1 }()\n\tfmt.Println(<-ch)\n}\n```\n\n")
	b.WriteString("## 链接与图片\n\n[示例链接](" + linkURL + ")\n\n![图一](local-image://smoke-a.png)\n\n![缺失的图](local-image://missing.png)\n\n")
	for i := 1; i <= 80; i++ {
		fmt.Fprintf(&b, "## 第 %d 节\n\n这是第 %d 节的正文，用来把文档撑长，核对滚动位置的恢复与上报。并发并发并发。\n\n", i, i)
	}
	return b.String()
}

// forumFixture 是论坛条目的正文：讨论总结、正文与两个楼层。
const forumFixture = "## 讨论总结\n\n大家认为应使用 VACUUM INTO。\n\n## 正文\n\n如何备份 WAL 模式的库？\n\n## 讨论\n\n**1 楼 · 张三**\n\n用 VACUUM INTO。\n\n**2 楼 · 李四**\n\n先 checkpoint。\n"

// galleryFixture 是图片条目的正文：画廊的图片引用从这里的 local-image:// 链接解析。
const galleryFixture = "![图一](local-image://smoke-a.png)\n\n![图二](local-image://smoke-b.png)\n"

// writeFixtureImages 在 dir 里写两张纯色渐变的 PNG（页面里用 naturalWidth 判断是否真的加载成功）。
func writeFixtureImages(dir string) error {
	for _, spec := range []struct {
		name string
		w, h int
		c    color.NRGBA
	}{
		{"smoke-a.png", 320, 200, color.NRGBA{R: 0x2E, G: 0x86, B: 0xDE, A: 0xFF}},
		{"smoke-b.png", 200, 300, color.NRGBA{R: 0xE5, G: 0x7A, B: 0x2B, A: 0xFF}},
	} {
		img := image.NewNRGBA(image.Rect(0, 0, spec.w, spec.h))
		for y := 0; y < spec.h; y++ {
			shade := uint8(160 + 95*y/spec.h)
			for x := 0; x < spec.w; x++ {
				img.SetNRGBA(x, y, color.NRGBA{R: scale8(spec.c.R, shade), G: scale8(spec.c.G, shade), B: scale8(spec.c.B, shade), A: 0xFF})
			}
		}
		f, err := os.Create(filepath.Join(dir, spec.name))
		if err != nil {
			return err
		}
		if err := png.Encode(f, img); err != nil {
			f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
	return nil
}

func scale8(v, shade uint8) uint8 { return uint8(int(v) * int(shade) / 255) }
