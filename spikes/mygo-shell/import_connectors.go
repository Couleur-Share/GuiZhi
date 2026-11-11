package main

// 对应 services/import/{connectors,media-files}.ts 里与网络无关的部分：文本、本地文件、媒体文件资产化。
// url 类来源（网页、视频、论坛、登录态平台）属于采集一族，经 Library.extractURL 接入。

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// extractedContent 对应 ExtractedContent（暂不含 webCapture）。
type extractedContent struct {
	Title          string
	Content        string
	ItemType       string
	SourceURI      *string
	Transcript     *string
	DegradedReason string
	WarningReason  string
}

const textFileMaxBytes = 5 * 1024 * 1024

var (
	imageExts = []string{".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp"}
	audioExts = []string{".mp3", ".wav", ".m4a", ".aac", ".ogg", ".flac"}
	videoExts = []string{".mp4", ".webm", ".mkv", ".mov", ".avi"}
)

var mediaSizeLimits = map[string]int64{"image": 20 << 20, "audio": 300 << 20, "video": 1024 << 20}

func classifyMediaFile(ext string) string {
	ext = strings.ToLower(ext)
	for kind, list := range map[string][]string{"image": imageExts, "audio": audioExts, "video": videoExts} {
		for _, e := range list {
			if e == ext {
				return kind
			}
		}
	}
	return ""
}

func mediaProtocolURL(kind, fileName string) string {
	if kind == "image" {
		return "local-image://" + fileName
	}
	return "local-video://" + fileName
}

func (p Paths) mediaAssetDir(kind string) string {
	if kind == "image" {
		return p.ImagesDir()
	}
	return p.VideosDir()
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// saveMediaAsset 把媒体文件按内容哈希拷进资产目录（已存在则复用），返回资产文件名。
func (p Paths) saveMediaAsset(src, kind string) (string, error) {
	dir := p.mediaAssetDir(kind)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	sum, err := fileSHA256(src)
	if err != nil {
		return "", err
	}
	name := "import-" + sum[:16] + strings.ToLower(nodeExtname(src))
	dst, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return name, nil // 命中已有资产：内容一致
		}
		return "", err
	}
	in, err := os.Open(src)
	if err != nil {
		dst.Close()
		return "", err
	}
	defer in.Close()
	_, err = io.Copy(dst, in)
	if cerr := dst.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(filepath.Join(dir, name))
		return "", err
	}
	return name, nil
}

func extractText(input string) *extractedContent {
	content := jsTrim(input)
	return &extractedContent{Title: firstLineTitle(content), Content: content, ItemType: "note"}
}

func (l *Library) extractMediaFile(resolved, kind string, size int64) (*extractedContent, error) {
	if limit := mediaSizeLimits[kind]; size > limit {
		return nil, fmt.Errorf("文件超过 %d MB 上限", int64(math.Round(float64(limit)/(1024*1024))))
	}
	asset, err := l.paths.saveMediaAsset(resolved, kind)
	if err != nil {
		return nil, err
	}
	url := mediaProtocolURL(kind, asset)
	base := filepath.Base(resolved)
	src := resolved
	if kind == "image" {
		return &extractedContent{Title: base, ItemType: "image", SourceURI: &src,
			Content: "![" + base + "](" + url + ")\n\n> 图片已导入本地资产库，可在详情页预览；配置视觉模型后可识别图中文字。"}, nil
	}
	label := "视频"
	if kind == "audio" {
		label = "音频"
	}
	return &extractedContent{Title: base, ItemType: kind, SourceURI: &src,
		Content: "[" + base + "](" + url + ")\n\n> " + label + "文件已导入本地资产库，可在详情页播放；配置「语音转写」模型后可生成文字稿。"}, nil
}

func (l *Library) extractFile(filePath string) (*extractedContent, error) {
	resolved := resolvePath(filePath)
	ext := strings.ToLower(nodeExtname(resolved))
	st, err := os.Stat(resolved)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, errors.New("不是有效的文件")
	}
	if kind := classifyMediaFile(ext); kind != "" {
		return l.extractMediaFile(resolved, kind, st.Size())
	}
	if ext != ".txt" && ext != ".md" && ext != ".markdown" {
		shown := ext
		if shown == "" {
			shown = "(无扩展名)"
		}
		return nil, fmt.Errorf("暂不支持的文件类型: %s（支持文本 / 图片 / 音频 / 视频）", shown)
	}
	if st.Size() > textFileMaxBytes {
		return nil, errors.New("文件超过 5MB 上限")
	}
	raw, err := os.ReadFile(resolved)
	if err != nil {
		return nil, err
	}
	content := jsTrim(decodeUTF8Lossy(raw))
	base := strings.TrimSuffix(filepath.Base(resolved), nodeExtname(resolved))
	title := firstLineTitle(content)
	if title == "" {
		title = base
	}
	itemType := "note"
	if ext == ".txt" {
		itemType = "document"
	}
	src := resolved
	return &extractedContent{Title: title, Content: content, ItemType: itemType, SourceURI: &src}, nil
}

// decodeUTF8Lossy 对应 fs.readFile(path, "utf8")：非法序列变 U+FFFD，开头的 BOM 保留（随后被 trim 去掉）。
func decodeUTF8Lossy(b []byte) string { return lossyUTF8(b) }

// defaultExtract 是 import:enqueue 使用的抽取入口。
func (l *Library) defaultExtract(ctx context.Context, task map[string]any, onStage func(string)) (*extractedContent, error) {
	kind, _ := task["sourceKind"].(string)
	input, _ := task["sourceInput"].(string)
	switch kind {
	case "text":
		return extractText(input), nil
	case "file":
		return l.extractFile(input)
	case "url":
		if l.extractURL != nil {
			return l.extractURL(ctx, task, onStage)
		}
		return nil, errors.New("该来源类型的采集尚未迁移到新内核")
	}
	return nil, fmt.Errorf("未知的采集类型: %s", kind)
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
