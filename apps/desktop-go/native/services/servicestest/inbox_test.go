package servicestest

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
)

// 处理中心假实现自身的行为：Store 的测试靠这些行为构造竞态与失败，它们不能悄悄变样。

func TestInboxZeroValueIsUsable(t *testing.T) {
	var f Inbox
	ctx := context.Background()
	list, err := f.List(ctx)
	if err != nil || len(list.Items) != 0 || list.Total != 0 {
		t.Fatalf("零值应是空列表：%+v %v", list, err)
	}
	if ready, err := f.WikiModelReady(ctx); err != nil || ready {
		t.Fatalf("零值时 Wiki 模型未就绪：%v %v", ready, err)
	}
	if st, err := f.SemanticStatus(ctx); err != nil || st.Configured || st.Pending() != 0 {
		t.Fatalf("零值时语义索引未配置：%+v %v", st, err)
	}
	if n, err := f.Organize(ctx, services.InboxOrganizeInput{ItemIDs: []string{"a", "b"}}); err != nil || n != 2 {
		t.Fatalf("Organize 缺省返回传入的条数：%d %v", n, err)
	}
	if n, err := f.AcknowledgeImportWarning(ctx, "t"); err != nil || n != 1 {
		t.Fatalf("AcknowledgeImportWarning 缺省返回 1：%d %v", n, err)
	}
	if found, err := f.RetryImportTask(ctx, "t"); err != nil || !found {
		t.Fatalf("RetryImportTask 缺省找得到任务：%v %v", found, err)
	}
	notice, err := f.RunSemanticIndexing(ctx)
	if err != nil || notice == nil || notice.Kind != services.InboxSemanticNothing {
		t.Fatalf("缺省回执是 nothing：%+v %v", notice, err)
	}
}

// 被卡住的读取返回的是「发出时」的数据：这样才能构造旧请求晚到的竞态。
func TestInboxHeldReadReturnsSnapshotFromRequestTime(t *testing.T) {
	var f Inbox
	f.SetList(services.InboxList{Items: []services.InboxItem{{ID: "old"}}, Total: 1})
	release := f.Hold("List")
	got := make(chan services.InboxList, 1)
	go func() {
		list, _ := f.List(context.Background())
		got <- list
	}()
	deadline := time.Now().Add(2 * time.Second)
	for f.CallCount("List") == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	f.SetList(services.InboxList{Items: []services.InboxItem{{ID: "new"}}, Total: 1})
	release()
	select {
	case list := <-got:
		if len(list.Items) != 1 || list.Items[0].ID != "old" {
			t.Fatalf("晚到的响应应是发出时的数据：%+v", list)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("放行后读取没有返回")
	}
	// 闸门只卡一次
	if list, _ := f.List(context.Background()); list.Items[0].ID != "new" {
		t.Fatalf("第二次调用不再被卡住，返回新数据：%+v", list)
	}
}

func TestInboxFailOnPerArgumentTakesPrecedence(t *testing.T) {
	var f Inbox
	ctx := context.Background()
	general, specific := errors.New("整个方法失败"), errors.New("只有 t1 失败")
	f.FailOn("RetryImportTask", general)
	f.FailOn("RetryImportTask:t1", specific)
	if _, err := f.RetryImportTask(ctx, "t1"); !errors.Is(err, specific) {
		t.Fatalf("针对参数的注入优先：%v", err)
	}
	if _, err := f.RetryImportTask(ctx, "t2"); !errors.Is(err, general) {
		t.Fatalf("其余参数落到整个方法的注入：%v", err)
	}
	f.FailOn("RetryImportTask", nil)
	f.FailOn("RetryImportTask:t1", nil)
	if found, err := f.RetryImportTask(ctx, "t1"); err != nil || !found {
		t.Fatalf("取消注入后恢复：%v %v", found, err)
	}
	if got := f.RetriedCalls(); !slices.Equal(got, []string{"t1"}) {
		t.Fatalf("失败的调用不记入结果：%v", got)
	}
	if n := f.CallCount("RetryImportTask"); n != 3 {
		t.Fatalf("调用记录应含三次（含失败的）：%d %v", n, f.Calls())
	}
}

func TestInboxHoldReturnsContextErrorOnCancel(t *testing.T) {
	var f Inbox
	f.Hold("MarkReviewed")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := f.MarkReviewed(ctx, []string{"a"})
		done <- err
	}()
	deadline := time.Now().Add(2 * time.Second)
	for f.CallCount("MarkReviewed") == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("取消应返回 context.Canceled：%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("取消后调用没有返回")
	}
	if len(f.ReviewedCalls()) != 0 {
		t.Fatal("被取消的调用不记入结果")
	}
}

func TestInboxClassifyBatchesProgressAndDefaultPlan(t *testing.T) {
	var f Inbox
	f.SetClassifyBatches(3)
	var progress [][2]int
	sources := []services.InboxAISource{{ItemID: "a"}, {ItemID: "b"}}
	plan, err := f.Classify(context.Background(), sources, []string{"x"}, func(done, total int) { progress = append(progress, [2]int{done, total}) })
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(progress, [][2]int{{1, 3}, {2, 3}, {3, 3}}) {
		t.Fatalf("进度回调：%v", progress)
	}
	if len(plan) != 2 || plan[0].ItemID != "a" || plan[1].ItemID != "b" || plan[0].CollectionName != defaultCollection {
		t.Fatalf("缺省计划与来源同序、归入默认合集：%+v", plan)
	}
	if got := f.ClassifyCalls(); len(got) != 1 || !slices.Equal(got[0].Existing, []string{"x"}) {
		t.Fatalf("应记录归类请求：%+v", got)
	}

	f.SetAssignments(map[string]string{"b": "指定"})
	plan, _ = f.Classify(context.Background(), sources, nil, nil)
	if plan[0].CollectionName != defaultCollection || plan[1].CollectionName != "指定" {
		t.Fatalf("指定的计划优先：%+v", plan)
	}
}

func TestInboxClassifyDefaultBatchesAre20PerBatch(t *testing.T) {
	var f Inbox
	sources := make([]services.InboxAISource, 45)
	var last [2]int
	if _, err := f.Classify(context.Background(), sources, nil, func(done, total int) { last = [2]int{done, total} }); err != nil {
		t.Fatal(err)
	}
	if last != [2]int{3, 3} {
		t.Fatalf("45 条按每批 20 条是 3 批：%v", last)
	}
}

func TestInboxClassifyHoldAfterBatchHonorsCancel(t *testing.T) {
	var f Inbox
	f.SetClassifyBatches(3)
	f.HoldClassifyAfter(1)
	ctx, cancel := context.WithCancel(context.Background())
	var progress [][2]int
	done := make(chan error, 1)
	go func() {
		_, err := f.Classify(ctx, []services.InboxAISource{{ItemID: "a"}}, nil, func(d, total int) { progress = append(progress, [2]int{d, total}) })
		done <- err
	}()
	deadline := time.Now().Add(2 * time.Second)
	for f.CallCount("Classify") == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond) // 让它走到第 1 批之后的闸门
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("取消应返回 context.Canceled：%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("取消后归类没有返回")
	}
	if !slices.Equal(progress, [][2]int{{1, 3}}) {
		t.Fatalf("取消前只应回报第 1 批：%v", progress)
	}
}

func TestInboxRunSemanticIndexingRejectsOverlap(t *testing.T) {
	var f Inbox
	ctx := context.Background()
	release := f.Hold("RunSemanticIndexing")
	first := make(chan *services.InboxSemanticNotice, 1)
	go func() {
		n, _ := f.RunSemanticIndexing(ctx)
		first <- n
	}()
	deadline := time.Now().Add(2 * time.Second)
	for !f.SemanticIndexing() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if n, err := f.RunSemanticIndexing(ctx); n != nil || err != nil {
		t.Fatalf("已有一轮在跑时返回 nil 回执：%+v %v", n, err)
	}
	release()
	select {
	case n := <-first:
		if n == nil || n.Kind != services.InboxSemanticNothing {
			t.Fatalf("第一轮的回执：%+v", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("放行后第一轮没有结束")
	}
	if f.SemanticIndexing() || f.IndexRuns() != 1 {
		t.Fatalf("结束后标记复位、只真正跑了一轮：indexing=%v runs=%d", f.SemanticIndexing(), f.IndexRuns())
	}
}

func TestInboxSeededResultsAreCopies(t *testing.T) {
	var f Inbox
	reasons := []string{"原因"}
	f.SetList(services.InboxList{Items: []services.InboxItem{{ID: "a", Reasons: reasons}}})
	reasons[0] = "被改了"
	list, _ := f.List(context.Background())
	list.Items[0].Reasons[0] = "又被改了"
	again, _ := f.List(context.Background())
	if again.Items[0].Reasons[0] != "原因" {
		t.Fatalf("种子与返回值都应是拷贝：%v", again.Items[0].Reasons)
	}
}

func TestInboxOrganizeInputMarshalsOnlySetFields(t *testing.T) {
	cases := []struct {
		in   services.InboxOrganizeInput
		want string
	}{
		{services.InboxOrganizeInput{ItemIDs: []string{"a"}}, `{"itemIds":["a"]}`},
		{services.InboxOrganizeInput{}, `{"itemIds":[]}`},
		{services.InboxOrganizeInput{ItemIDs: []string{"a"}, CollectionID: "c"}, `{"collectionId":"c","itemIds":["a"]}`},
		{services.InboxOrganizeInput{ItemIDs: []string{"a"}, AddTagNames: []string{"x", "y"}}, `{"addTagNames":["x","y"],"itemIds":["a"]}`},
	}
	for _, c := range cases {
		raw, err := c.in.MarshalJSON()
		if err != nil || string(raw) != c.want {
			t.Fatalf("MarshalJSON(%+v) = %s %v，期望 %s", c.in, raw, err, c.want)
		}
	}
}
