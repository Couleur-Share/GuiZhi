package library

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/internal/frametime"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/reader"
)

// TestDetailEditorFrameBudget 读出编辑态（岛里是编辑器，假页面）下详情的帧耗时（M2-6a 的模块完成定义第 4 条：稳态帧 ≤ 2 ms）。
//
// 编辑器本身（CodeMirror）在岛的页面进程里，不在这里的读数里；宿主一侧多出来的是：每帧对编辑视图的期望比较，
// 每次按键一条带着全文的 edit 事件（解析、校验、记草稿，在界面线程上）。量三件事：进编辑态（点「编辑」到宿主把编辑视图发给页面，
// 含全文的 JSON 往返）、稳态帧（页面已握手、编辑中、内容不变）、一次按键（全文 size 字节的 edit 事件加一帧；
// 交替两份只差一个字符的事件，序列化在循环之外）。
//
// 耗时与机器有关，默认跳过：VIVA_FRAME_BUDGET=1 go test ./viva/screens/library -run TestDetailEditorFrameBudget -v -count=1。
func TestDetailEditorFrameBudget(t *testing.T) {
	if os.Getenv("VIVA_FRAME_BUDGET") == "" {
		t.Skip("帧耗时读数与机器有关，默认不跑：VIVA_FRAME_BUDGET=1 时运行")
	}
	for _, size := range []int{30 << 10, 1 << 20} {
		name := fmt.Sprintf("editor_10000_entries_%dKiB_doc", size>>10)
		t.Run(name, func(t *testing.T) { measureEditorFrames(t, name, size) })
	}
}

// measureEditorFrames 在 1 万条的库里把第一条的正文设成 size 字节，选中它、让页面握手、点「编辑」，再逐项量帧。
func measureEditorFrames(t *testing.T, name string, size int) {
	entries := servicestest.GenerateEntries(10000)
	article := readerArticle(0, size)
	rr := newReaderRig(t, func(r *rig) {
		r.svc.SeedEntries(entries)
		r.svc.SeedDetail(entries[0].ID, servicestest.DetailBody{Content: article})
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
	rr.clickAt(cx, cy)
	rr.frames(4)
	p := rr.page()
	p.send(t, map[string]any{"t": "ready", "v": reader.ProtocolVersion})
	rr.frames(3)

	// 进编辑态：点读者工具条的「编辑」钮，到宿主把编辑视图（带全文）发给页面。
	editName := locales.ThemedReadingReaderEditOriginal()
	if !rr.has(gfx.RoleButton, editName) {
		editName = locales.ThemedReadingReaderEditSummary()
	}
	start := time.Now()
	rr.click(gfx.RoleButton, editName)
	rr.frames(4)
	enter := time.Since(start)
	if v := lastView(t, p); v["kind"] != "editor" {
		t.Fatalf("点「编辑」之后页面应收到编辑视图，实际 %v", v["kind"])
	}
	t.Logf("%s 进编辑态（点击 + 4 帧，含全文 %d KiB 的 JSON 往返）：%.2f ms", name, size>>10, float64(enter)/float64(time.Millisecond))

	idle, err := frametime.Measure(300, func(int) error { return rr.session.Frame() })
	if err != nil {
		t.Fatalf("稳态帧失败：%v", err)
	}
	t.Logf("%s 编辑态稳态帧（布局+记录，页面已握手）：%s", name, idle)

	// 一次按键：页面把改动后的全文交回宿主（edit 事件），宿主记草稿并出一帧。
	seq := p.last(t, "view")["seq"]
	raw := [2]string{}
	for i, tail := range []string{"a", "b"} {
		b, err := json.Marshal(map[string]any{"t": "edit", "seq": seq, "text": article + tail})
		if err != nil {
			t.Fatal(err)
		}
		raw[i] = string(b)
	}
	posts := len(p.posts)
	keystroke, err := frametime.Measure(100, func(i int) error {
		p.cfg.OnMessage(raw[i%2])
		return rr.session.Frame()
	})
	if err != nil {
		t.Fatalf("按键帧失败：%v", err)
	}
	t.Logf("%s 一次按键（全文 %d KiB 的 edit 事件 + 出帧）：%s", name, size>>10, keystroke)
	if got := len(p.posts) - posts; got != 0 {
		t.Errorf("页面自己报上来的改动不该再被整篇回灌：期间又发给页面 %d 条消息", got)
	}
}
