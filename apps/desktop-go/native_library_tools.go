package main

// 文章工具侧栏（viva/screens/library）与主包之间的适配层：来源评论的读取与重新采集、网页来源版本的读取与采用。
// 与渲染进程走同一份处理器、同一套错误；DTO 与处理器输出的对应关系由适配器测试用真实处理器守住（native_library_tools_test.go）。

import (
	"context"
	"errors"

	"guizhi-mygo-shell/native/services"
)

// SourceComments 对应 platformCapture:listComments：只读已落库的评论，不联网。
func (a libraryAdapter) SourceComments(ctx context.Context, itemID string) ([]services.LibrarySourceComment, error) {
	return callAs[[]services.LibrarySourceComment](ctx, a.b, "platformCapture:listComments", itemID)
}

// RefreshSourceComments 对应 platformCapture:refreshComments：联网重新采集并落库，返回采集后的全部评论。
// 处理器自己校验 limit（只接受 10、20、50）与条目的平台来源，失败作为 error 返回。
func (a libraryAdapter) RefreshSourceComments(ctx context.Context, itemID string, limit int) ([]services.LibrarySourceComment, error) {
	return callAs[[]services.LibrarySourceComment](ctx, a.b, "platformCapture:refreshComments", map[string]any{"itemId": itemID, "limit": limit})
}

// WebVersions 对应 web:versions：回复是 { ok, data, error }，业务失败在 ok / error 里，不是 Go 错误。
func (a libraryAdapter) WebVersions(ctx context.Context, itemID string) (services.LibraryWebVersions, error) {
	reply, err := callAs[struct {
		OK    bool                         `json:"ok"`
		Error string                       `json:"error"`
		Data  *services.LibraryWebVersions `json:"data"`
	}](ctx, a.b, "web:versions", itemID)
	if err != nil {
		return services.LibraryWebVersions{}, err
	}
	if !reply.OK || reply.Data == nil {
		if reply.Error != "" {
			return services.LibraryWebVersions{}, errors.New(reply.Error)
		}
		return services.LibraryWebVersions{}, errors.New("读取原文版本失败")
	}
	return *reply.Data, nil
}

// AdoptWebVersion 对应 web:adopt：回复是 { ok, error }。
func (a libraryAdapter) AdoptWebVersion(ctx context.Context, in services.LibraryAdoptWebVersionInput) error {
	reply, err := callAs[struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}](ctx, a.b, "web:adopt", in)
	if err != nil {
		return err
	}
	if !reply.OK {
		if reply.Error != "" {
			return errors.New(reply.Error)
		}
		return errors.New("采用原文版本失败")
	}
	return nil
}
