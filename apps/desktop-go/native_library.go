package main

// 知识库侧栏（viva/screens/library）与主包之间的适配层：把 collection:*、tag:*、knowledge:counts 这几个
// 现有的 IPC 处理器接成 services.Library。与渲染进程走同一份逻辑、同一套错误，DTO 与处理器输出的对应关系
// 由适配器测试用真实处理器守住（native_library_test.go）。

import (
	"context"
	"errors"

	"guizhi-mygo-shell/native/services"
)

// libraryAdapter 把知识库侧栏用到的处理器接成 services.Library。
//
// hub 是推送分发（进度订阅用，见 native_library_media.go）；为空时没有推送，只影响 SubscribeMediaProgress。
type libraryAdapter struct {
	b   *Bridge
	hub *pushHub
}

var _ services.Library = libraryAdapter{}

func (a libraryAdapter) Counts(ctx context.Context, q services.LibraryFacetQuery) (services.LibraryCounts, error) {
	return callAs[services.LibraryCounts](ctx, a.b, "knowledge:counts", q)
}

func (a libraryAdapter) List(ctx context.Context, q services.LibraryListQuery) (services.LibraryPage, error) {
	return callAs[services.LibraryPage](ctx, a.b, "knowledge:list", q)
}

func (a libraryAdapter) Collections(ctx context.Context) ([]services.LibraryCollection, error) {
	return callAs[[]services.LibraryCollection](ctx, a.b, "collection:list")
}

// iconArg 把图标写成处理器的入参：空串是「没有图标」，渲染层传 null。
func iconArg(icon string) any {
	if icon == "" {
		return nil
	}
	return icon
}

func (a libraryAdapter) CreateCollection(ctx context.Context, name, icon string) (services.LibraryCollection, error) {
	return callAs[services.LibraryCollection](ctx, a.b, "collection:create", map[string]any{"name": name, "icon": iconArg(icon)})
}

func (a libraryAdapter) UpdateCollection(ctx context.Context, id, name, icon string) error {
	_, err := a.b.call(ctx, "collection:update", id, map[string]any{"name": name, "icon": iconArg(icon)})
	return err
}

func (a libraryAdapter) DeleteCollection(ctx context.Context, id string) (bool, error) {
	return callAs[bool](ctx, a.b, "collection:delete", id)
}

func (a libraryAdapter) Tags(ctx context.Context) ([]services.LibraryTag, error) {
	return callAs[[]services.LibraryTag](ctx, a.b, "tag:list")
}

func (a libraryAdapter) UpdateTag(ctx context.Context, id string, patch services.LibraryTagPatch) error {
	arg := map[string]any{}
	if patch.Name != nil {
		arg["name"] = *patch.Name
	}
	if patch.ColorKey != nil {
		arg["colorKey"] = *patch.ColorKey
	}
	_, err := a.b.call(ctx, "tag:update", id, arg)
	return err
}

func (a libraryAdapter) DeleteTag(ctx context.Context, id string) (bool, error) {
	return callAs[bool](ctx, a.b, "tag:delete", id)
}

// ---- 条目操作（M2-3）：批量、收藏与标签、补采、AI 交接稿 ----

// selectionReply 是 knowledge:selection 的回复（freeze 与 execute 共用一个形状）。
type selectionReply struct {
	OK      bool                          `json:"ok"`
	Error   string                        `json:"error"`
	IDs     []string                      `json:"ids"`
	Results []services.LibraryBatchResult `json:"results"`
}

// Freeze 对应 library-workflow.store 的冻结：回复缺 ids 视为失败，原因取处理器给的。
// 处理器出错时一定带原因，这里的兜底文案（渲染层各调用点各有一句）实际走不到。
func (a libraryAdapter) Freeze(ctx context.Context, q services.LibraryListQuery) ([]string, error) {
	reply, err := callAs[selectionReply](ctx, a.b, "knowledge:selection", map[string]any{"action": "freeze", "query": q})
	if err != nil {
		return nil, err
	}
	if !reply.OK || reply.IDs == nil {
		if reply.Error != "" {
			return nil, errors.New(reply.Error)
		}
		return nil, errors.New("冻结选择失败")
	}
	return reply.IDs, nil
}

func (a libraryAdapter) ExecuteBatch(ctx context.Context, runID string, ids []string, cmd services.LibraryBatchCommand) ([]services.LibraryBatchResult, error) {
	input := map[string]any{"action": "execute", "ids": ids, "command": cmd}
	if runID != "" {
		input["runId"] = runID
	}
	reply, err := callAs[selectionReply](ctx, a.b, "knowledge:selection", input)
	if err != nil {
		return nil, err
	}
	if !reply.OK || reply.Results == nil {
		if reply.Error != "" {
			return nil, errors.New(reply.Error)
		}
		return nil, errors.New("批量操作未返回结果")
	}
	return reply.Results, nil
}

func (a libraryAdapter) UpdateEntry(ctx context.Context, id string, patch services.LibraryEntryPatch) (services.LibraryEntry, bool, error) {
	entry, err := callAs[*services.LibraryEntry](ctx, a.b, "knowledge:update", id, patch)
	if err != nil || entry == nil {
		return services.LibraryEntry{}, false, err
	}
	return *entry, true, nil
}

// SupplementWechat 只关心入队是否成功；要跟踪任务进度用 SupplementWechatTasks（同一个频道，见 native_library_snapshot.go）。
func (a libraryAdapter) SupplementWechat(ctx context.Context, ids []string) error {
	_, err := a.SupplementWechatTasks(ctx, ids)
	return err
}

// AiHandoff 对应 use-ai-handoff.ts 的 build：按 id 重新取完整条目（列表里没有正文与文字稿），再按条目整理。
func (a libraryAdapter) AiHandoff(ctx context.Context, id string, full bool) (services.LibraryHandoff, error) {
	item, err := callAs[*struct {
		Title        string `json:"title"`
		Content      string `json:"content"`
		Transcript   string `json:"transcript"`
		Summary      string `json:"summary"`
		ItemType     string `json:"itemType"`
		SourceURI    string `json:"sourceUri"`
		CollectionID string `json:"collectionId"`
		CreatedAt    int64  `json:"createdAt"`
		Tags         []struct {
			Name string `json:"name"`
		} `json:"tags"`
	}](ctx, a.b, "knowledge:get", id)
	if err != nil {
		return services.LibraryHandoff{}, err
	}
	if item == nil {
		return services.LibraryHandoff{}, services.ErrLibraryItemMissing
	}
	if jsTrim(item.Content) == "" && jsTrim(item.Transcript) == "" {
		return services.LibraryHandoff{}, services.ErrHandoffEmpty
	}
	h := aiHandoffItem{
		Title: item.Title, Content: item.Content, Transcript: item.Transcript, Summary: item.Summary,
		ItemType: item.ItemType, SourceURI: item.SourceURI, CreatedAtMs: item.CreatedAt,
	}
	for _, t := range item.Tags {
		h.Tags = append(h.Tags, t.Name)
	}
	if item.CollectionID != "" {
		// 合集名取不到（合集读取失败）时按「没有合集」处理：交接稿少一行归属，不因此拒绝复制。
		if collections, err := a.Collections(ctx); err == nil {
			for _, c := range collections {
				if c.ID == item.CollectionID {
					h.Collection = c.Name
					break
				}
			}
		}
	}
	doc := buildAiHandoff(h, full)
	return services.LibraryHandoff{Title: item.Title, Text: doc.Text, CharCount: doc.CharCount, OmittedChars: doc.OmittedChars}, nil
}

func (a libraryAdapter) ExportAiHandoff(ctx context.Context, title, text string) (services.LibraryExportResult, error) {
	reply, err := callAs[struct {
		Success  bool   `json:"success"`
		Canceled bool   `json:"canceled"`
		FilePath string `json:"filePath"`
		Error    string `json:"error"`
	}](ctx, a.b, "export:aiHandoff", map[string]any{"title": title, "text": text})
	if err != nil {
		return services.LibraryExportResult{}, err
	}
	return services.LibraryExportResult{Success: reply.Success, Canceled: reply.Canceled, FilePath: reply.FilePath, Error: reply.Error}, nil
}
