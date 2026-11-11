package main

// 主题阅读素材：原图补采、主题生图与落盘。对应 services/themed-reading/assets.ts。
// 图片落盘用「写临时文件 → 硬链接到内容寻址文件名」：同名已存在则必须字节一致，否则拒绝覆盖共享资源。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const themedImageTimeout = 480 * time.Second

var reHTTPURL = regexp.MustCompile(`(?i)^https?://`)

// themedAssetCheckpoint 在素材状态变化后保存检查点（运行时提供）。
type themedAssetCheckpoint func() error

func (l *Library) publishThemeImage(ctx context.Context, data []byte, asset *jsObj, checkpoint themedAssetCheckpoint, cleanup func(string)) (err error) {
	ext, err := themedImageExtension(data)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	fileName := "theme-" + sha + "." + ext
	release, err := retainAssetFiles([]string{fileName})
	if err != nil {
		return err
	}
	staged := filepath.Join(l.paths.ImagesDir(), ".theme-"+themedUUID()+".tmp")
	committed := false
	previous := themedCopyObj(asset)
	defer func() {
		_ = os.Remove(staged)
		release()
		if !committed {
			for _, k := range asset.keys() {
				asset.delete(k)
			}
			for _, k := range previous.keys() {
				asset.set(k, previous.get(k))
			}
			cleanup(fileName)
		}
	}()
	if err := os.MkdirAll(l.paths.ImagesDir(), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(staged, data, 0o644); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	target, err := l.localThemeAssetPath(fileName)
	if err != nil {
		return err
	}
	if err := os.Link(staged, target); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		existing, rerr := os.ReadFile(target)
		if rerr != nil {
			return rerr
		}
		esum := sha256.Sum256(existing)
		if len(existing) != len(data) || hex.EncodeToString(esum[:]) != sha {
			return errors.New("同名主题图片已损坏，未覆盖共享资源")
		}
	}
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	asset.set("fileName", fileName)
	asset.set("sha256", sha)
	asset.set("bytes", float64(len(data)))
	asset.set("status", "ready")
	asset.set("error", jsUndefined)
	if err := checkpoint(); err != nil {
		return err
	}
	committed = true
	return nil
}

// prepareOriginalAsset 校验已保存的原图；缺失且有 http(s) 原地址时补采，本地图片不猜测替代来源。
func (l *Library) prepareOriginalAsset(ctx context.Context, asset *jsObj, checkpoint themedAssetCheckpoint, cleanup func(string)) error {
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	originalURL := themedStr(asset, "originalUrl")
	if name := themedStr(asset, "fileName"); name != "" {
		err := func() error {
			release, err := retainAssetFiles([]string{name})
			if err != nil {
				return err
			}
			defer release()
			data, _, err := l.readThemeAsset(asset)
			if err != nil {
				return err
			}
			if ctx.Err() != nil {
				return context.Cause(ctx)
			}
			sum := sha256.Sum256(data)
			asset.set("sha256", hex.EncodeToString(sum[:]))
			asset.set("bytes", float64(len(data)))
			asset.set("status", "ready")
			asset.set("error", jsUndefined)
			return checkpoint()
		}()
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		if !reHTTPURL.MatchString(originalURL) {
			return fmt.Errorf("原图无法读取，请重新采集来源图片：%s", err.Error())
		}
		// 只对已保存的原始网络地址补采，不猜测本地图片的替代来源。
	}
	if !reHTTPURL.MatchString(originalURL) {
		return errors.New("原图地址不受支持，未发送请求")
	}
	f := l.webFetcher
	if f == nil {
		f = defaultSafeFetcher
	}
	dir, file, err := f.downloadToTempFile(ctx, originalURL, "image", themedImageLimit, safeRequestOptions{Accept: "image/*"})
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	return l.publishThemeImage(ctx, data, asset, checkpoint, cleanup)
}

// generateThemeAsset 调用生图模型并落盘；超时 / 取消给出「结果未知，手动继续会再次调用」的说明。
func (l *Library) generateThemeAsset(ctx context.Context, asset *jsObj, direction string, cfg aiClientConfig, checkpoint themedAssetCheckpoint, cleanup func(string), onRequest func() error) error {
	prompt := fmt.Sprintf("Theme illustration for an article. Visual direction: %s\nPurpose: %s\nComposition: %s\nDo not render text, letters, numbers, pseudo-text, labels or watermarks. This is an illustration, not evidence. Respect the requested background and negative space.",
		direction, themedStr(asset, "purpose"), themedStr(asset, "prompt"))
	gen := l.generateImage
	if l.imageFn != nil {
		gen = l.imageFn
	}
	img, err := gen(ctx, prompt, themedStr(asset, "aspectRatio"), cfg, imageGenOptions{Scenario: "themedReading", Timeout: themedImageTimeout, OnRequest: onRequest}.withNoRetry())
	if err != nil {
		msg := err.Error()
		if ctx.Err() != nil || strings.Contains(strings.ToLower(msg), "timeout") || strings.Contains(strings.ToLower(msg), "timed out") || strings.Contains(msg, "超时") {
			return fmt.Errorf("主题生图未取得完整结果（单次等待上限 %d 秒）。请求可能已提交，服务端结果未知；手动继续会再次调用。%s", int(themedImageTimeout.Seconds()), msg)
		}
		return err
	}
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	return l.publishThemeImage(ctx, img.Data, asset, checkpoint, cleanup)
}
