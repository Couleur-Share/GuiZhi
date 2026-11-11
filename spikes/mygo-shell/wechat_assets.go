package main

// 对应 services/web-capture/snapshot-assets.ts：把公众号正文里的图片落到资产目录，
// 文件名 wechat-<sha256>.<ext>，同卷暂存后以硬链接原子发布；落盘即租约，防止孤儿清理误删。
// 已知差异：SVG 图片 TS 经 resvg 栅格化成 PNG，这里不转换，按「不是支持的图片格式」记入失败清单
// （文章仍保存，标记为不完整，可重试补采）。

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

type snapshotAsset struct {
	FileName  string
	SHA256    string
	SourceURL string
	Bytes     int64
}

type snapshotFailure struct {
	URL    string
	Reason string
}

const (
	snapshotMaxAssets    = 200
	snapshotMaxAssetSize = 20 << 20
	snapshotMaxTotalSize = 100 << 20
)

// snapshotImageExt 对应 extension()：按魔数识别 png / jpg / gif / webp。
func snapshotImageExt(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte{137, 80, 78, 71, 13, 10, 26, 10}):
		return "png"
	case len(data) >= 3 && data[0] == 255 && data[1] == 216 && data[2] == 255:
		return "jpg"
	case bytes.HasPrefix(data, []byte("GIF87a")) || bytes.HasPrefix(data, []byte("GIF89a")):
		return "gif"
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "webp"
	}
	return ""
}

// collectSnapshotAssets 对应 collectSnapshotAssets：返回已保存资产、失败清单与统一释放租约的函数。
// 顺序约定与 TS 一致：资产按完成顺序追加；超出 200 个与取消/超时的条目记入失败。
func (l *Library) collectSnapshotAssets(ctx context.Context, urls []string) ([]snapshotAsset, []snapshotFailure, func()) {
	assets := []snapshotAsset{}
	failures := []snapshotFailure{}
	seen := map[string]bool{}
	unique := []string{}
	for _, u := range urls {
		if !seen[u] {
			seen[u] = true
			unique = append(unique, u)
		}
	}
	candidates := unique
	if len(candidates) > snapshotMaxAssets {
		candidates = unique[:snapshotMaxAssets]
		for _, u := range unique[snapshotMaxAssets:] {
			failures = append(failures, snapshotFailure{u, "每篇最多保存 200 个资源"})
		}
	}
	var (
		mu       sync.Mutex
		next     int
		total    int64
		releases []func()
	)
	release := func() {
		mu.Lock()
		defer mu.Unlock()
		for _, r := range releases {
			r()
		}
		releases = nil
	}
	imagesDir := l.paths.ImagesDir()
	if err := os.MkdirAll(imagesDir, 0o755); err != nil {
		for _, u := range candidates {
			failures = append(failures, snapshotFailure{u, err.Error()})
		}
		return assets, failures, release
	}
	fetcher := l.webFetcher
	if fetcher == nil {
		fetcher = defaultSafeFetcher
	}
	one := func(url string) {
		fail := func(err error) {
			reason := "图片保存失败"
			if err != nil && err.Error() != "" {
				reason = err.Error()
			}
			mu.Lock()
			failures = append(failures, snapshotFailure{url, reason})
			mu.Unlock()
		}
		mu.Lock()
		over := total >= snapshotMaxTotalSize
		mu.Unlock()
		if over {
			fail(errors.New("文章资源超过 100 MiB"))
			return
		}
		dir, file, err := fetcher.downloadToTempFile(ctx, url, "image", snapshotMaxAssetSize, safeRequestOptions{Referer: "https://mp.weixin.qq.com/"})
		if err != nil {
			fail(err)
			return
		}
		defer os.RemoveAll(dir)
		data, err := os.ReadFile(file)
		if err != nil {
			fail(err)
			return
		}
		ext := snapshotImageExt(data)
		if ext == "" {
			fail(errors.New("资源不是支持的图片格式"))
			return
		}
		mu.Lock()
		total += int64(len(data))
		tooBig := int64(len(data)) > snapshotMaxAssetSize || total > snapshotMaxTotalSize
		mu.Unlock()
		if tooBig {
			fail(errors.New("图片或文章资源超过大小上限"))
			return
		}
		if ctx.Err() != nil {
			fail(ctx.Err())
			return
		}
		sum := sha256.Sum256(data)
		hash := hex.EncodeToString(sum[:])
		name := "wechat-" + hash + "." + ext
		var rnd [16]byte
		_, _ = rand.Read(rnd[:])
		staged := filepath.Join(imagesDir, ".wechat-"+hex.EncodeToString(rnd[:])+".tmp")
		werr := os.WriteFile(staged, data, 0o644)
		if werr == nil {
			if lerr := os.Link(staged, filepath.Join(imagesDir, name)); lerr != nil && !errors.Is(lerr, fs.ErrExist) {
				werr = lerr
			}
		}
		_ = os.Remove(staged)
		if werr != nil {
			fail(werr)
			return
		}
		rel, rerr := retainAssetFiles([]string{name})
		if rerr != nil {
			fail(rerr)
			return
		}
		mu.Lock()
		releases = append(releases, rel)
		assets = append(assets, snapshotAsset{FileName: name, SHA256: hash, SourceURL: url, Bytes: int64(len(data))})
		mu.Unlock()
	}
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				mu.Lock()
				if next >= len(candidates) || ctx.Err() != nil {
					mu.Unlock()
					return
				}
				url := candidates[next]
				next++
				mu.Unlock()
				one(url)
			}
		}()
	}
	wg.Wait()
	for _, u := range candidates[min(next, len(candidates)):] {
		failures = append(failures, snapshotFailure{u, "采集已取消或超时"})
	}
	return assets, failures, release
}
