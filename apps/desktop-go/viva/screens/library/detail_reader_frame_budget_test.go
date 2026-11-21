package library

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/internal/frametime"
	"guizhi-mygo-shell/viva/reader"
)

// TestDetailReaderFrameBudget 读出接了阅读岛（假页面）之后详情的帧耗时（M2-5 的模块完成定义第 4 条：稳态帧 ≤ 2 ms、滚动一步 ≤ 6 ms）。
//
// 与 TestDetailFrameBudget 的区别：阅读区是接了岛宿主的（假打开器，页面由测试扮演），所以每帧多了 readerRequest、
// 宿主的差量比较与摆放（Place）；岛里页面自己的渲染在另一个进程里，不在这里的读数里（无头会话也画不出岛）。
// 量四件事：稳态帧（页面已握手、内容不变）、↓ 切换一步（宿主把新内容发给页面）、页面每步上报一次阅读位置（宿主存偏好）、
// 以及一篇约 1 MB 的长文打开时的单次耗时与之后的稳态帧（正文很大时宿主每帧不该再逐字节比较或重切）。
//
// 耗时与机器有关，默认跳过：VIVA_FRAME_BUDGET=1 go test ./viva/screens/library -run TestDetailReaderFrameBudget -v -count=1。
func TestDetailReaderFrameBudget(t *testing.T) {
	if os.Getenv("VIVA_FRAME_BUDGET") == "" {
		t.Skip("帧耗时读数与机器有关，默认不跑：VIVA_FRAME_BUDGET=1 时运行")
	}
	t.Run("reader_10000_entries", func(t *testing.T) { measureReaderFrames(t, "reader_10000_entries", 30<<10) })
	t.Run("reader_10000_entries_1MB_article", func(t *testing.T) { measureReaderFrames(t, "reader_10000_entries_1MB_article", 1<<20) })
}

// readerArticle 生成约 size 字节的 Markdown 正文（标题、段落、列表与代码块交替），seed 区分各篇。
func readerArticle(seed int, size int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# 文章 %d\n\n", seed)
	for n := 0; b.Len() < size; n++ {
		fmt.Fprintf(&b, "## 第 %d 节\n\n这是第 %d 篇的第 %d 段正文，用来撑出长度；包含 **加粗**、`行内代码` 与 [链接](https://example.com/%d/%d)。\n\n- 要点一\n- 要点二\n\n```go\nfunc f%d() int { return %d }\n```\n\n", n, seed, n, seed, n, n, n)
	}
	return b.String()
}

// measureReaderFrames 在 1 万条的库里把前 20 条（一页）的正文设成 size 字节，选中第一条、让页面握手，再逐项量帧。
func measureReaderFrames(t *testing.T, name string, size int) {
	heapBefore := frametime.HeapInUse()
	entries := servicestest.GenerateEntries(10000)
	rr := newReaderRig(t, func(r *rig) {
		r.svc.SeedEntries(entries)
		for i := 0; i < 20; i++ {
			r.svc.SeedDetail(entries[i].ID, servicestest.DetailBody{Content: readerArticle(i, size)})
		}
	})
	listed := rr.view().List.Entries
	if len(listed) < 3 {
		t.Fatalf("夹具应读到一页条目，实际 %d 条", len(listed))
	}
	card, ok := rr.cardOf(listed[0])
	if !ok {
		t.Fatal("找不到第一张卡片（夹具失效）")
	}
	cx, cy := centerOf(card)

	// 打开第一条：从点击到宿主把第一份 view 发给页面（页面先握手），计一次耗时。
	start := time.Now()
	rr.clickAt(cx, cy)
	rr.frames(4)
	p := rr.page()
	p.send(t, map[string]any{"t": "ready", "v": reader.ProtocolVersion})
	rr.frames(3)
	open := time.Since(start)
	if got := len(p.messages(t, "view")); got == 0 {
		t.Fatalf("握手之后页面应已收到 view；收到过 %s", p.kinds(t))
	}
	if got := rr.view().Detail; got.Item == nil || got.Item.ID != listed[0].ID {
		t.Fatalf("点卡片之后详情应是 %s：%+v", listed[0].ID, got)
	}
	t.Logf("%s 打开（点击 + 4 帧 + 握手 + 3 帧，含假页面的 JSON 往返）：%.2f ms，正文 %d KiB", name, float64(open)/float64(time.Millisecond), size>>10)

	t.Logf("%s 打开文章后 Go 堆增量（含无头会话与假页面；WebView2 的内存在另一个进程里，不在这里）：%d KiB", name, (int64(frametime.HeapInUse())-int64(heapBefore))/1024)

	idle, err := frametime.Measure(300, func(int) error { return rr.session.Frame() })
	if err != nil {
		t.Fatalf("稳态帧失败：%v", err)
	}
	t.Logf("%s 稳态帧（布局+记录，页面已握手）：%s", name, idle)

	// ↓ 切换一步：选中项换一篇，详情重取，宿主把新内容发给页面；先预检页面确实收到了不同条目的 view。
	viewsBefore := len(p.messages(t, "view"))
	if err := rr.session.Keys("down"); err != nil {
		t.Fatalf("预检按键失败：%v", err)
	}
	rr.frames(3)
	if len(p.messages(t, "view")) == viewsBefore {
		t.Fatalf("按 ↓ 之后页面没有收到新的 view，读数无效；收到过 %s", p.kinds(t))
	}
	switchStep, err := frametime.Measure(18, func(i int) error {
		key := "down"
		if i >= 9 {
			key = "up"
		}
		if err := rr.session.Keys(key); err != nil {
			return err
		}
		return rr.session.Frame()
	})
	if err != nil {
		t.Fatalf("切换帧失败：%v", err)
	}
	t.Logf("%s ↓ 切换一步（按键 + 详情重排 + 出帧）：%s", name, switchStep)

	// 页面每步上报一次阅读位置（真页面防抖 200 ms，这里是上界）：宿主记进偏好。
	seq := p.last(t, "view")["seq"] // 取一次：p.last 要把已发的消息逐条解 JSON（长文是 1 MB 一条），放进循环会把测试自己的开销算进读数
	scroll, err := frametime.Measure(100, func(i int) error {
		p.send(t, map[string]any{"t": "scroll", "seq": seq, "top": float64(i * 40)})
		return rr.session.Frame()
	})
	if err != nil {
		t.Fatalf("滚动上报帧失败：%v", err)
	}
	t.Logf("%s 页面上报一次阅读位置（事件 + 出帧）：%s", name, scroll)
}
