package main

// 显示设置里「背景图」的主包适配器（native/services/appearance_images.go）。
//
// 与 native_adapters.go 的其余适配器同一做法：经 Bridge.call 调已登记的 IPC 处理器（app_runtime.go，与渲染进程同一份逻辑、同一套校验），
// 行为只有一个来源，对应关系由 native_settings_appearance_test.go 用真实处理器与临时数据目录守住。
//
//	BackgroundImages.PickAndSave → dialog:selectImage（取第一张）+ image:save（只存刚选过的路径，返回应用图片目录里的纯文件名）
//	BackgroundImages.Read        → image:readBase64（validateImageFileName：拒绝路径穿越与非法字符），外加扩展名白名单与明确的失败原因
//
// 与渲染层的差别只有一处：image:readBase64 对非法或不存在的文件回 null（渲染层据此画损坏的回退图），
// 这里把 null 翻译成带原因的错误，让界面能区分「读不出来」与「空图」，绝不静默返回空字节。

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"

	"guizhi-mygo-shell/native/services"
)

type backgroundImagesAdapter struct {
	b *Bridge
	// pickMu 串行化选图与存图：image:save 只认「最近一次选图」登记的路径（imageState 单槽），两次并发的选图会互相冲掉。
	pickMu sync.Mutex
}

var _ services.BackgroundImages = (*backgroundImagesAdapter)(nil)

func newBackgroundImagesAdapter(b *Bridge) *backgroundImagesAdapter {
	return &backgroundImagesAdapter{b: b}
}

func (a *backgroundImagesAdapter) PickAndSave(ctx context.Context) (string, error) {
	a.pickMu.Lock()
	defer a.pickMu.Unlock()
	paths, err := callAs[[]string](ctx, a.b, "dialog:selectImage")
	if err != nil {
		return "", err
	}
	if len(paths) == 0 || paths[0] == "" {
		return "", nil // 用户取消
	}
	saved, err := callAs[[]string](ctx, a.b, "image:save", []string{paths[0]})
	if err != nil {
		return "", err
	}
	if len(saved) == 0 || saved[0] == "" {
		// 处理器会静默跳过没经过选图白名单、扩展名不对或读不出来的文件，结果是空列表：渲染层把它当保存失败。
		return "", errors.New("the selected image could not be saved")
	}
	return saved[0], nil
}

func (a *backgroundImagesAdapter) Read(ctx context.Context, fileName string) ([]byte, error) {
	// 用与 image:readBase64 同一份校验给出明确原因（处理器自己对任何失败都只回 null）；目录只用于拼路径，这里不使用。
	if _, err := validateImageFileName(fileName, "images"); err != nil {
		return nil, fmt.Errorf("background image %q rejected: %w", fileName, err)
	}
	if !imageExtensions[strings.ToLower(nodeExtname(fileName))] {
		return nil, fmt.Errorf("background image %q rejected: unsupported image type", fileName)
	}
	encoded, err := callAs[*string](ctx, a.b, "image:readBase64", fileName)
	if err != nil {
		return nil, err
	}
	if encoded == nil {
		return nil, fmt.Errorf("background image %q not found or unreadable", fileName)
	}
	data, err := base64.StdEncoding.DecodeString(*encoded)
	if err != nil {
		return nil, fmt.Errorf("background image %q is not valid base64: %w", fileName, err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("background image %q is empty", fileName)
	}
	return data, nil
}
