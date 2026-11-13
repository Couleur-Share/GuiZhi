package main

// 图文作品的条目组装：配图落盘资产库 + 逐图 OCR。抖音与小红书共用这一份。对应 import/image-note-entry.ts。
//
// 图文的信息主体常常在图里（信息图、指标对比表、架构流程图），只保留文案等于把内容主体丢了；
// 而平台的图片地址都带时效签名，不落盘的话过期就再也取不回来——所以采集时必须把图拉进资产库。
// OCR 让图里的文字进入全文检索与语义索引；逐图识别按张计费，因此设了张数上限。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	// 逐图识别按张计费，超出部分只入库不识别
	ocrImageLimit = 9
	// 失败原因写进正文时的截断长度
	noteErrorMaxLen = 120
)

// imageNoteSource：组装一条图文条目所需的全部素材（由各平台的采集模块产出）。
type imageNoteSource struct {
	PlatformLabel string // 正文元数据引用块里的平台名
	Title         string
	// 标题是作者在平台上自己写的（小红书有独立标题字段）；为 false（抖音只能取文案首行）才 AI 重拟
	AuthoredTitle bool
	Description   string // 完整文案；与标题相同时为空
	Author        string
	ImageMirrors  [][]string // 逐张图片的 CDN 镜像地址（同一张图给多个源，下载时逐个降级）
	WebpageURL    string
	// 单张配图的下载实现：各平台的 UA / Referer / 扩展名判定都不一样
	DownloadImage func(ctx context.Context, mirrors []string) (dir, filePath string, err error)
}

type imageNoteDeps struct {
	downloadImage  func(ctx context.Context, mirrors []string) (dir, filePath string, err error)
	saveAsset      func(filePath string) (string, error)
	getOcrConfig   func() *ocrModelConfig
	recognize      func(ctx context.Context, filePath string, c ocrModelConfig) (string, error)
	getTitleConfig func() *aiClientConfig
	// generateTitle 返回空串表示模型没给出有效标题
	generateTitle func(ctx context.Context, source string, cfg aiClientConfig) (string, error)
	onStage       func(string)
}

func (d *imageNoteDeps) stage(s string) {
	if d.onStage != nil {
		d.onStage(s)
	}
}

var (
	reOrderedListLine = regexp.MustCompile(`^(\d{1,9})[.)]` + fsp)
	reMDBlockStart    = regexp.MustCompile("^(#{1,6}(" + fsp + "|$)|[-+*]" + fsp + "|>|```|~~~|-{3,}$|={3,}$|_{3,}$)")
)

// escapeMarkdownBlockStart 转义行首会被 Markdown 当成块级标记的形态：标题、列表、引用、代码围栏、分隔线。
func escapeMarkdownBlockStart(line string) string {
	// 有序列表 `1.` / `1)`：只能转义标点，`\1` 不是合法转义
	if m := reOrderedListLine.FindStringSubmatch(line); m != nil {
		return m[1] + `\` + line[len(m[1]):]
	}
	if reMDBlockStart.MatchString(line) {
		return `\` + line
	}
	return line
}

// plainTextToMarkdown 纯文本转 Markdown 正文：逐行转义行首标记再用空行分段，渲染结果与原文逐行对齐。
func plainTextToMarkdown(text string) string {
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if line = jsTrim(line); line != "" {
			out = append(out, escapeMarkdownBlockStart(line))
		}
	}
	return strings.Join(out, "\n\n")
}

// buildImageNoteMetaLine 正文顶部的元数据引用块内容（形状与视频条目一致，详情页共用解析）。
func buildImageNoteMetaLine(s imageNoteSource) string {
	parts := []string{"平台：" + s.PlatformLabel}
	if s.Author != "" {
		parts = append(parts, "作者："+s.Author)
	}
	parts = append(parts, fmt.Sprintf("图文 %d 张", len(s.ImageMirrors)))
	return strings.Join(parts, " · ")
}

type noteImage struct {
	assetFileName string
	assetPath     string // 资产文件的绝对路径，OCR 直接读它
	err           string // 下载失败时的可读原因；有值时前两个字段为空
}

func (l *Library) collectNoteImages(ctx context.Context, src imageNoteSource, deps *imageNoteDeps) ([]noteImage, error) {
	download := deps.downloadImage
	if download == nil {
		download = src.DownloadImage
	}
	save := deps.saveAsset
	if save == nil {
		save = func(filePath string) (string, error) { return l.paths.saveMediaAsset(filePath, "image") }
	}
	var images []noteImage
	for _, mirrors := range src.ImageMirrors {
		img, err := func() (noteImage, error) {
			dir, file, err := download(ctx, mirrors)
			if dir != "" {
				defer os.RemoveAll(dir)
			}
			if err != nil {
				return noteImage{}, err
			}
			name, err := save(file)
			if err != nil {
				return noteImage{}, err
			}
			return noteImage{assetFileName: name, assetPath: filepath.Join(l.paths.mediaAssetDir("image"), name)}, nil
		}()
		if err != nil {
			if isCanceledError(err) {
				return nil, err
			}
			// 单张失败不该让整条采集失败：其余图片与文案仍然有价值
			img = noteImage{err: err.Error()}
		}
		images = append(images, img)
	}
	return images, nil
}

type ocrOutcome struct {
	texts     []*string // 每张图的识别结果（未识别或失败的为 nil）
	attempted int
	failed    int
	firstErr  string // 首个失败原因，用于在正文里如实交代
	hasFirst  bool
}

// recognizeNoteImages 逐图识别；单张失败不影响其余。
func (l *Library) recognizeNoteImages(ctx context.Context, images []noteImage, cfg ocrModelConfig, deps *imageNoteDeps) (ocrOutcome, error) {
	recognize := deps.recognize
	if recognize == nil {
		recognize = l.recognizeImageFile
	}
	out := ocrOutcome{}
	for i, img := range images {
		if img.assetPath == "" || i >= ocrImageLimit {
			out.texts = append(out.texts, nil)
			continue
		}
		out.attempted++
		text, err := recognize(ctx, img.assetPath, cfg)
		if err != nil {
			if ctx.Err() != nil {
				return out, errCanceledMsg
			}
			fmt.Printf("[import] 第 %d 张图 OCR 失败: %v\n", i+1, err)
			out.failed++
			if !out.hasFirst {
				out.firstErr, out.hasFirst = err.Error(), true
			}
			out.texts = append(out.texts, nil)
			continue
		}
		t := text
		out.texts = append(out.texts, &t)
	}
	return out, nil
}

// buildOcrNotes 识别情况的正文注记。全军覆没时必须说明原因——否则用户无从判断是模型没配、额度用完还是图里本就没字。
func buildOcrNotes(o ocrOutcome) []string {
	if o.failed == 0 {
		return nil
	}
	reason := utf16Slice(o.firstErr, noteErrorMaxLen)
	if o.failed == o.attempted {
		return []string{"> 图中文字识别失败：" + reason + "。可在详情页点「识别图中文字」重试。"}
	}
	return []string{fmt.Sprintf("> 有 %d 张图的文字识别失败（%s），可在详情页重试。", o.failed, reason)}
}

func buildImageSection(images []noteImage) []string {
	out := make([]string, 0, len(images))
	for i, img := range images {
		if img.assetPath != "" {
			out = append(out, fmt.Sprintf("![图 %d](%s)", i+1, mediaProtocolURL("image", img.assetFileName)))
		} else {
			out = append(out, fmt.Sprintf("> 第 %d 张图下载失败：%s", i+1, img.err))
		}
	}
	return out
}

func buildOcrSection(images []noteImage, texts []*string) []string {
	var parts []string
	for i, t := range texts {
		if t == nil || *t == "" {
			continue
		}
		if len(parts) == 0 {
			parts = append(parts, ocrSectionHeading)
		}
		// 只有一张图时不必再分小标题
		if len(images) > 1 {
			parts = append(parts, fmt.Sprintf("### 图 %d", i+1))
		}
		parts = append(parts, *t)
	}
	return parts
}

// buildImageNoteEntry 组装图文条目：元数据引用块 + 文案 + 配图 + 图中文字。条目类型用 image，详情页才会出现重新识别入口。
func (l *Library) buildImageNoteEntry(ctx context.Context, src imageNoteSource, deps imageNoteDeps) (*extractedContent, error) {
	deps.stage("image-download")
	images, err := l.collectNoteImages(ctx, src, &deps)
	if err != nil {
		return nil, err
	}
	var ocrCfg *ocrModelConfig
	if deps.getOcrConfig != nil {
		ocrCfg = deps.getOcrConfig()
	} else {
		ocrCfg = l.resolveOcrConfig()
	}
	outcome := ocrOutcome{texts: make([]*string, len(images))}
	if ocrCfg != nil {
		deps.stage("image-ocr")
		if outcome, err = l.recognizeNoteImages(ctx, images, *ocrCfg, &deps); err != nil {
			return nil, err
		}
	}

	parts := []string{"> " + buildImageNoteMetaLine(src)}
	caption := src.Description
	if caption == "" {
		caption = src.Title
	}
	if c := plainTextToMarkdown(caption); c != "" {
		parts = append(parts, c)
	}
	parts = append(parts, buildImageSection(images)...)
	if ocrCfg == nil {
		parts = append(parts, "> 未配置「视觉模型」，图中文字尚未识别；配置后可在详情页点「识别图中文字」。")
	} else {
		parts = append(parts, buildOcrNotes(outcome)...)
		if len(images) > ocrImageLimit {
			parts = append(parts, fmt.Sprintf("> 图片较多，仅识别了前 %d 张的文字，其余图片已入库可在详情页查看。", ocrImageLimit))
		}
	}
	parts = append(parts, buildOcrSection(images, outcome.texts)...)

	title, err := l.resolveNoteTitle(ctx, src, outcome.texts, &deps)
	if err != nil {
		return nil, err
	}
	uri := src.WebpageURL
	return &extractedContent{Title: title, Content: strings.Join(parts, "\n\n"), ItemType: "image", SourceURI: &uri}, nil
}

// resolveNoteTitle：作者写过标题的（小红书）照用，不重拟；抖音没有标题字段，原始标题只是文案首行、往往是半句话，
// 有文本素材且配了文本模型时改用 AI 拟题；拟题失败不阻断导入，退回文案首行。
func (l *Library) resolveNoteTitle(ctx context.Context, src imageNoteSource, texts []*string, deps *imageNoteDeps) (string, error) {
	if src.AuthoredTitle {
		return src.Title, nil
	}
	var cfg *aiClientConfig
	if deps.getTitleConfig != nil {
		cfg = deps.getTitleConfig()
	} else {
		cfg = l.resolveMediaSummaryConfig()
	}
	if cfg == nil {
		return src.Title, nil
	}
	first := src.Description
	if first == "" {
		first = src.Title
	}
	pieces := []string{first}
	for _, t := range texts {
		if t != nil && *t != "" {
			pieces = append(pieces, *t)
		}
	}
	material := jsTrim(strings.Join(pieces, "\n\n"))
	if material == "" {
		return src.Title, nil
	}
	deps.stage("summarizing")
	generate := deps.generateTitle
	if generate == nil {
		generate = func(ctx context.Context, source string, c aiClientConfig) (string, error) {
			title, ok, err := l.generateContentTitle(ctx, source, c, nil)
			if err != nil || !ok {
				return "", err
			}
			return title, nil
		}
	}
	title, err := generate(ctx, material, *cfg)
	if err != nil {
		if ctx.Err() != nil {
			return "", errCanceledMsg
		}
		fmt.Printf("[import] 图文 AI 拟题失败，保留文案首行: %v\n", err)
		return src.Title, nil
	}
	if title == "" {
		return src.Title, nil
	}
	return title, nil
}
