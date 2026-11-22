package main

// 条目详情（viva/screens/library 的详情栏）与主包之间的适配层：把 knowledge:get、knowledge:saveDraft 与
// knowledge:selection 的 source-versions 接成 services.Library 的三个方法。与渲染进程走同一份处理器、同一套错误；
// DTO 与处理器输出的对应关系由适配器测试用真实处理器守住（native_library_detail_test.go）。

import (
	"context"
	"errors"

	"guizhi-mygo-shell/native/services"
)

// GetItem 对应 knowledge:get：条目不存在时处理器返回 null。
func (a libraryAdapter) GetItem(ctx context.Context, id string) (services.LibraryItem, bool, error) {
	item, err := callAs[*services.LibraryItem](ctx, a.b, "knowledge:get", id)
	if err != nil || item == nil {
		return services.LibraryItem{}, false, err
	}
	return *item, true, nil
}

// CreateItem 对应 knowledge:create：新建空白笔记，只带合集；处理器返回新条目的完整形态。
func (a libraryAdapter) CreateItem(ctx context.Context, in services.LibraryCreateInput) (services.LibraryItem, error) {
	payload := map[string]any{}
	if in.CollectionID != "" {
		payload["collectionId"] = in.CollectionID
	}
	item, err := callAs[*services.LibraryItem](ctx, a.b, "knowledge:create", payload)
	if err != nil {
		return services.LibraryItem{}, err
	}
	if item == nil {
		return services.LibraryItem{}, errors.New("新建条目没有返回结果")
	}
	return *item, nil
}

// SaveDraft 对应 knowledge:saveDraft：业务上的失败（冲突、条目已不存在、参数无效）在回复的 ok / error 里，不是 Go 错误。
func (a libraryAdapter) SaveDraft(ctx context.Context, in services.LibraryDraftInput) (services.LibraryDraftResult, error) {
	return callAs[services.LibraryDraftResult](ctx, a.b, "knowledge:saveDraft", in)
}

// SourceVersions 对应 knowledge:selection 的 source-versions。
func (a libraryAdapter) SourceVersions(ctx context.Context, id string) ([]services.LibrarySourceVersion, error) {
	reply, err := callAs[struct {
		OK       bool                            `json:"ok"`
		Error    string                          `json:"error"`
		Versions []services.LibrarySourceVersion `json:"versions"`
	}](ctx, a.b, "knowledge:selection", map[string]any{"action": "source-versions", "id": id})
	if err != nil {
		return nil, err
	}
	if !reply.OK {
		if reply.Error != "" {
			return nil, errors.New(reply.Error)
		}
		return nil, errors.New("读取来源版本失败")
	}
	return reply.Versions, nil
}

// MediaCapabilities 对应 media:capabilities：语音转写路由的可选能力（{ diarization: boolean }）。
func (a libraryAdapter) MediaCapabilities(ctx context.Context) (services.LibraryMediaCapabilities, error) {
	return callAs[services.LibraryMediaCapabilities](ctx, a.b, "media:capabilities")
}
