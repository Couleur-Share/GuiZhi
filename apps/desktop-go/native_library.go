package main

// 知识库侧栏（viva/screens/library）与主包之间的适配层：把 collection:*、tag:*、knowledge:counts 这几个
// 现有的 IPC 处理器接成 services.Library。与渲染进程走同一份逻辑、同一套错误，DTO 与处理器输出的对应关系
// 由适配器测试用真实处理器守住（native_library_test.go）。

import (
	"context"

	"guizhi-mygo-shell/native/services"
)

// libraryAdapter 把知识库侧栏用到的处理器接成 services.Library。
type libraryAdapter struct{ b *Bridge }

var _ services.Library = libraryAdapter{}

func (a libraryAdapter) Counts(ctx context.Context, q services.LibraryFacetQuery) (services.LibraryCounts, error) {
	return callAs[services.LibraryCounts](ctx, a.b, "knowledge:counts", q)
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
