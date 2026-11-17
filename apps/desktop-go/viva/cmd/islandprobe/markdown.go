package main

import (
	"fmt"
	"hash/fnv"
	"image"
	"runtime"
	"sort"
	"strings"
	"time"

	vapp "github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/markdown"
)

// markdownArticle 生成约 size 字节的中文 Markdown 长文：标题、段落、列表、引用、代码块与表格交替出现
// （与 pages.go 里的 HTML 长文同一份素材，便于和岛里的渲染对比）。
func markdownArticle(size int) string {
	sentences := []string{
		"知识库的价值不在于收集了多少内容，而在于需要的时候能不能在三秒之内找回它，并且还记得当初为什么要保存它。",
		"采集到的网页往往带着导航栏、广告和评论区，阅读视图要做的第一件事是把正文从这些噪声里分离出来，再按统一的排版呈现。",
		"检索质量取决于切块的粒度：块太大，向量会被无关内容稀释；块太小，上下文又断得支离破碎，所以通常按语义边界切并保留少量重叠。",
		"本地优先意味着数据首先属于用户的磁盘，网络只是可选的增强；备份、迁移和导出都应该是一条命令就能完成的普通操作。",
		"当模型的回答引用了某条笔记，界面必须能一键跳回原文并高亮被引用的段落，否则用户没有办法判断答案是否可信。",
		"长文阅读最容易暴露性能问题：几千行的文本、几十个代码块、再加上表格和图片，滚动时每一帧都要在十六毫秒之内完成。",
		"错误处理要让用户看得懂、能重试：失败的原因要写清楚，可以复制，批量操作要逐条记账，而不是笼统地提示完成。",
		"配置里凡是离开数据目录的机密字段都必须加密保存，日志里不允许出现密钥与口令，导出的备份也不携带这些内容。",
	}
	var b strings.Builder
	b.WriteString("# 长文阅读性能探针\n\n")
	for i := 0; b.Len() < size; i++ {
		switch i % 7 {
		case 0:
			fmt.Fprintf(&b, "## 第 %d 节 · %s\n\n", i/7+1, sentences[i%len(sentences)][:18])
		case 3:
			for j := 0; j < 4; j++ {
				fmt.Fprintf(&b, "- %s\n", sentences[(i+j)%len(sentences)])
			}
			b.WriteString("\n")
		case 4:
			fmt.Fprintf(&b, "> %s\n\n", sentences[(i*3)%len(sentences)])
		case 5:
			b.WriteString("```go\n")
			for j := 0; j < 22; j++ {
				fmt.Fprintf(&b, "func step%d_%d(ctx context.Context, items []Item) error { // 第 %d 行\n\tfor _, it := range items { if err := handle(ctx, it); err != nil { return err } }\n", i, j, j)
			}
			b.WriteString("```\n\n")
		case 6:
			b.WriteString("| 指标 | 当前 | 目标 | 备注 |\n| --- | --- | --- | --- |\n")
			for j := 0; j < 8; j++ {
				fmt.Fprintf(&b, "| 指标 %d-%d | %d ms | %d ms | %s |\n", i, j, 10+j*3, 8+j*2, sentences[(i+j)%len(sentences)][:24])
			}
			b.WriteString("\n")
		default:
			fmt.Fprintf(&b, "%s%s\n\n", sentences[i%len(sentences)], sentences[(i+3)%len(sentences)])
		}
	}
	return b.String()
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

// hashPix 返回整帧像素的 FNV-1a 哈希与「与左上角底色不同的像素」占比（证明画出来的不是空白）。
func hashPix(img *image.RGBA) (uint64, float64) {
	h := fnv.New64a()
	_, _ = h.Write(img.Pix)
	bg := img.Pix[0:3]
	inked := 0
	for i := 0; i+3 < len(img.Pix); i += 4 {
		if img.Pix[i] != bg[0] || img.Pix[i+1] != bg[1] || img.Pix[i+2] != bg[2] {
			inked++
		}
	}
	return h.Sum64(), float64(inked) / float64(len(img.Pix)/4)
}

// quantiles 返回 p50、p95、p99 与最大值（毫秒，保留两位）。
func quantiles(values []float64) map[string]float64 {
	if len(values) == 0 {
		return map[string]float64{}
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	q := func(p float64) float64 {
		i := int(p * float64(len(sorted)))
		if i >= len(sorted) {
			i = len(sorted) - 1
		}
		return float64(int(sorted[i]*1000)) / 1000
	}
	return map[string]float64{"p50": q(0.5), "p95": q(0.95), "p99": q(0.99), "max": float64(int(sorted[len(sorted)-1]*1000)) / 1000}
}

// runMarkdownBench 用 Viva 自带的 Markdown 组件无头渲染 50 KB 与 200 KB 的中文长文：
// 解析与排版耗时、逐帧耗时（只画可见区）、滚动步耗时、跨块选择与复制。
func runMarkdownBench() map[string]any {
	res := map[string]any{}
	for _, size := range []int{50 << 10, 200 << 10} {
		res[fmt.Sprintf("%dKB", size>>10)] = benchMarkdown(markdownArticle(size))
	}
	return res
}

func benchMarkdown(src string) map[string]any {
	out := map[string]any{"sourceBytes": len(src)}
	var doc markdown.Document
	var view components.MarkdownView
	look := components.MarkdownLookFor(gfx.LightTheme())
	style := components.MarkdownStyle{Size: 16, LineHeight: 28}

	var (
		laidOut    bool
		scrollY    float32
		docHeight  float32
		parseDur   time.Duration
		layoutDur  time.Duration
		paintTimes []float64
		paintCalls int
		unclipped  bool
		heapBefore runtime.MemStats
		heapAfter  runtime.MemStats
	)
	config := vapp.Config{
		Title: "markdown-bench", Width: 960, Height: 640,
		Paint: func(canvas *gfx.Canvas, size gfx.Size) {
			if !laidOut {
				runtime.GC()
				runtime.ReadMemStats(&heapBefore)
				t0 := time.Now()
				doc.Parse(src)
				parseDur = time.Since(t0)
				t1 := time.Now()
				docHeight = view.Layout(canvas, style, &doc, size.Width-96)
				layoutDur = time.Since(t1)
				runtime.ReadMemStats(&heapAfter)
				laidOut = true
			}
			t := time.Now()
			clip := gfx.Rect{Width: size.Width, Height: size.Height}
			if unclipped {
				clip.Height = docHeight + 100 // 整篇都送进显示列表：一帧里有几百上千个不同的文字行（计划 §4.7 的 #184 复现条件）
			}
			view.Paint(canvas, components.MarkdownPaint{Look: look, X: 48, Y: -scrollY, Clip: clip})
			paintTimes = append(paintTimes, ms(time.Since(t)))
			paintCalls++
		},
	}
	session, err := headless.Open(config, nil, headless.Options{Scale: 1})
	if err != nil {
		out["openError"] = err.Error()
		return out
	}
	defer session.Close()
	if err := session.Frame(); err != nil {
		out["firstFrameError"] = err.Error()
		return out
	}
	out["parseMs"] = ms(parseDur)
	out["layoutMs"] = ms(layoutDur)
	out["docHeightPx"] = docHeight
	out["heapDeltaMB"] = float64(int64(heapAfter.HeapAlloc)-int64(heapBefore.HeapAlloc)) / (1 << 20)

	// 注意：Session.Frame() 只「记录」一帧（跑 Paint 回调、生成显示列表），不栅格化；
	// 真正的绘制（字形上图集、D3D 提交）发生在 Session.Image() 里（同一条 Direct3D 离屏路径，另含回读，是真窗口的上界）。
	// 所以下面分「记录」与「渲染」两类计时，并用图像哈希证明画出来的不是空白。

	// 静止帧：同一画面连记 120 帧。
	paintTimes = paintTimes[:0]
	callsBefore := paintCalls
	var frameTimes []float64
	for i := 0; i < 120; i++ {
		t := time.Now()
		if err := session.Frame(); err != nil {
			out["frameError"] = err.Error()
			return out
		}
		frameTimes = append(frameTimes, ms(time.Since(t)))
	}
	out["steadyRecordMs"] = quantiles(frameTimes)
	out["steadyViewPaintMs"] = quantiles(paintTimes)
	out["steadyPaintCalls"] = paintCalls - callsBefore

	// 滚动（只记录）：每步 40px，来回走满整篇 600 步。
	paintTimes = paintTimes[:0]
	frameTimes = frameTimes[:0]
	callsBefore = paintCalls
	dir := float32(1)
	maxY := docHeight - 640
	for i := 0; i < 600; i++ {
		scrollY += 40 * dir
		if scrollY >= maxY {
			scrollY, dir = maxY, -1
		} else if scrollY <= 0 {
			scrollY, dir = 0, 1
		}
		t := time.Now()
		if err := session.Frame(); err != nil {
			out["scrollError"] = err.Error()
			return out
		}
		frameTimes = append(frameTimes, ms(time.Since(t)))
	}
	out["scrollRecordMs"] = quantiles(frameTimes)
	out["scrollViewPaintMs"] = quantiles(paintTimes)
	out["scrollPaintCalls"] = paintCalls - callsBefore

	// 渲染：第一次 Image 含 D3D 离屏渲染器的建立，单独记。
	scrollY = 0
	if err := session.Frame(); err != nil {
		out["frameError"] = err.Error()
		return out
	}
	t0 := time.Now()
	first, err := session.Image()
	if err != nil {
		out["imageError"] = err.Error()
		return out
	}
	out["firstImageMs"] = ms(time.Since(t0))
	firstHash, firstInk := hashPix(first)
	out["firstImageInkRatio"] = float64(int(firstInk*10000)) / 10000

	// 页翻（640px 一步）走完整篇两遍：第一遍字形图集是冷的，第二遍是热的；每步分别记「记录」「渲染」「合计」。
	hashes := map[uint64]bool{firstHash: true}
	pass := func(name string) {
		var record, render, total []float64
		steps := 0
		for y := float32(0); y <= maxY; y += 640 {
			scrollY = y
			a := time.Now()
			if err := session.Frame(); err != nil {
				out[name+"Error"] = err.Error()
				return
			}
			b := time.Now()
			img, err := session.Image()
			if err != nil {
				out[name+"Error"] = err.Error()
				return
			}
			c := time.Now()
			record = append(record, ms(b.Sub(a)))
			render = append(render, ms(c.Sub(b)))
			total = append(total, ms(c.Sub(a)))
			if steps%7 == 0 {
				h, _ := hashPix(img)
				hashes[h] = true
			}
			steps++
		}
		out[name] = map[string]any{"steps": steps, "recordMs": quantiles(record), "renderMs": quantiles(render), "totalMs": quantiles(total)}
	}
	pass("pageDownCold")
	pass("pageDownWarm")
	out["distinctImageHashes"] = len(hashes)

	// 滚轮步（40px 一步，字形已热）：每步「记录 + 渲染」合计。
	var wheel []float64
	scrollY = 0
	for i := 0; i < 240; i++ {
		scrollY += 40
		if scrollY > maxY {
			scrollY = 0
		}
		a := time.Now()
		if err := session.Frame(); err != nil {
			out["wheelError"] = err.Error()
			return out
		}
		if _, err := session.Image(); err != nil {
			out["wheelError"] = err.Error()
			return out
		}
		wheel = append(wheel, ms(time.Since(a)))
	}
	out["wheelStepRenderTotalMs"] = quantiles(wheel)

	// #184 复现条件：一帧里有 512 个以上不同的文字行时，Windows 文本整形缓存是否崩塌（逐帧耗时陡增）。
	// 做法：不裁剪，整篇进显示列表，连续 8 帧；对比前面「只画可见区」的同类数字。崩塌的表现是每帧都要重新整形全部文字行。
	unclipped = true
	scrollY = 0
	var uRecord, uRender []float64
	for i := 0; i < 8; i++ {
		a := time.Now()
		if err := session.Frame(); err != nil {
			out["unclippedError"] = err.Error()
			return out
		}
		b := time.Now()
		if _, err := session.Image(); err != nil {
			out["unclippedError"] = err.Error()
			return out
		}
		uRecord = append(uRecord, ms(b.Sub(a)))
		uRender = append(uRender, ms(time.Since(b)))
	}
	unclipped = false
	out["unclippedRecordMs"] = uRecord
	out["unclippedRenderMs"] = uRender

	// 跨块选择与复制：整篇选中取文本；再用坐标反查偏移（最近一次 Paint 的原点）。
	t := time.Now()
	text := view.SelectedText(components.MarkdownSelection{Anchor: 0, Focus: view.TextLen()})
	out["selectAllCopyMs"] = ms(time.Since(t))
	out["selectAllCopyChars"] = len([]rune(text))
	if off, ok := view.OffsetAt(gfx.Point{X: 200, Y: 300}); ok {
		out["offsetAtPoint"] = off
	}
	out["findInPage"] = "组件没有页内查找（需要自己在 plain 文本上做并滚动到位）"
	return out
}
