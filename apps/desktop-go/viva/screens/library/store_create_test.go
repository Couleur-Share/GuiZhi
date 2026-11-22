package library

import (
	"errors"
	"slices"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
)

// 本文件对应渲染层 knowledge.store 的 createItem（快速采集的「空白笔记」）：先落盘当前草稿，新建后刷新列表、选中新条目并把光标放进标题。

// 新建成功：带着合集建出空白笔记，列表刷新后能看到它，详情选中它且光标落进标题。
func TestCreateItemSelectsTheNewBlankNote(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	if err := h.store.CreateItem(h.ctx, "c1"); err != nil {
		t.Fatalf("新建：%v", err)
	}
	creates := h.svc.Creates()
	if len(creates) != 1 || creates[0].CollectionID != "c1" {
		t.Fatalf("新建参数：%+v", creates)
	}
	v := h.store.View()
	d := v.Detail
	if d.Item == nil || d.Item.ID == "a" || d.Item.CollectionID != "c1" || d.Item.Content != "" || d.Loading || d.LoadError != "" {
		t.Fatalf("详情应是新建的空白笔记：%+v", d)
	}
	if v.List.SelectedID != d.Item.ID || d.TitleFocusID != d.Item.ID {
		t.Fatalf("应选中新条目并把光标放进标题：selected=%q titleFocus=%q", v.List.SelectedID, d.TitleFocusID)
	}
	if !slices.ContainsFunc(v.List.Entries, func(e services.LibraryEntry) bool { return e.ID == d.Item.ID }) {
		t.Fatalf("列表应已刷新、包含新条目：%+v", v.List.Entries)
	}
	h.store.ClearTitleFocus()
	if h.detail().TitleFocusID != "" {
		t.Fatal("消费之后光标请求应清掉")
	}
}

// 不选合集：参数里没有合集（未归合集）。
func TestCreateItemWithoutCollection(t *testing.T) {
	h := detailHarness(t)
	if err := h.store.CreateItem(h.ctx, ""); err != nil {
		t.Fatalf("新建：%v", err)
	}
	if creates := h.svc.Creates(); len(creates) != 1 || creates[0].CollectionID != "" {
		t.Fatalf("新建参数：%+v", creates)
	}
	if d := h.detail(); d.Item == nil || d.Item.CollectionID != "" {
		t.Fatalf("详情：%+v", d)
	}
}

// 当前草稿先落盘再新建：改动不会因为切到新条目而丢。
func TestCreateItemFlushesThePendingDraftFirst(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.edit(patchTitle("甲改"))
	if err := h.store.CreateItem(h.ctx, ""); err != nil {
		t.Fatalf("新建：%v", err)
	}
	if got, _ := h.svc.ItemNow("a"); got.Title != "甲改" {
		t.Fatalf("草稿应先落盘：%+v", got)
	}
	calls := h.svc.Calls()
	save, create := slices.Index(calls, "SaveDraft:a"), slices.Index(calls, "CreateItem")
	if save < 0 || create < 0 || save > create {
		t.Fatalf("应先保存草稿再新建：%v", calls)
	}
}

// 草稿落不了盘：不新建（免得丢编辑），把原因交给调用方，选中不动。
func TestCreateItemStopsWhenTheDraftCannotBeSaved(t *testing.T) {
	h := detailHarness(t)
	h.open("a")
	h.edit(patchTitle("甲改"))
	h.svc.FailOn("SaveDraft", errors.New("磁盘已满"))
	err := h.store.CreateItem(h.ctx, "")
	if err == nil {
		t.Fatal("草稿落盘失败应返回错误")
	}
	if len(h.svc.Creates()) != 0 {
		t.Fatalf("不应新建：%+v", h.svc.Creates())
	}
	if h.store.View().List.SelectedID != "a" {
		t.Fatalf("选中不应变：%q", h.store.View().List.SelectedID)
	}
}

// 服务端新建失败：错误原文返回给调用方，选中不动。
func TestCreateItemReportsAServiceFailure(t *testing.T) {
	h := detailHarness(t)
	h.open("b")
	h.svc.FailOn("CreateItem", errors.New("数据库只读"))
	err := h.store.CreateItem(h.ctx, "")
	if err == nil || err.Error() != "数据库只读" {
		t.Fatalf("应返回服务端的原因：%v", err)
	}
	if h.store.View().List.SelectedID != "b" || h.detail().TitleFocusID != "" {
		t.Fatalf("失败后选中与光标请求都不变：%+v", h.store.View())
	}
}

// 新建途中用户又选了别的条目：新条目照样建成、列表照常刷新，但不抢走选中。
func TestCreateItemDoesNotStealALaterSelection(t *testing.T) {
	h := detailHarness(t)
	release := h.svc.HoldList()
	done := make(chan error, 1)
	go func() { done <- h.store.CreateItem(h.ctx, "") }()
	deadline := time.Now().Add(5 * time.Second)
	for !slices.Contains(h.svc.Calls(), "CreateItem") {
		if time.Now().After(deadline) {
			t.Fatal("新建请求没有发出")
		}
		time.Sleep(time.Millisecond)
	}
	h.store.SelectItem("c")
	release()
	if err := <-done; err != nil {
		t.Fatalf("新建：%v", err)
	}
	if v := h.store.View(); v.List.SelectedID != "c" || v.Detail.Item == nil || v.Detail.Item.ID != "c" || v.Detail.TitleFocusID != "" {
		t.Fatalf("应保持用户后来选的条目：%+v", v.Detail)
	}
	if len(h.svc.Creates()) != 1 {
		t.Fatalf("条目仍应建成：%+v", h.svc.Creates())
	}
}
