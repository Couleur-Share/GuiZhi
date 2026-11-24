package library

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/internal/frametime"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/reader"
)

// editorScaleDoc 造一份恰好 units 个 UTF-16 码元的文档。wide 为真时是纯汉字（每码元 3 字节，8 Mi 码元正好是 MaxEditTextBytes 的 24 MiB）；
// 否则是中英混排的 Markdown 正文（接近真实笔记，大部分是 ASCII），末尾用 ASCII 补齐到整数个码元。
func editorScaleDoc(units int, wide bool) string {
	if wide {
		return strings.Repeat("字", units)
	}
	var b strings.Builder
	b.Grow(units + 512)
	count := 0
	for n := 0; ; n++ {
		chunk := fmt.Sprintf("## 第 %d 节\n\n这是第 %d 段正文，用来撑出长度；包含 **加粗**、`行内代码` 与 [链接](https://example.com/%d)。\n\n- 要点一\n- 要点二\n\n```go\nfunc f%d() int { return %d }\n```\n\n", n, n, n, n, n)
		u := len(utf16.Encode([]rune(chunk)))
		if count+u > units {
			break
		}
		b.WriteString(chunk)
		count += u
	}
	b.WriteString(strings.Repeat("a", units-count))
	return b.String()
}

// TestDetailEditorScale 量岛里编辑器在 1 / 4 / 8 Mi 个 UTF-16 码元的文档上的宿主一侧成本（台账 B-070，M4 评估）。
//
// 页面（CodeMirror）在岛的进程里，这里只量得到宿主一侧：每次按键页面把全文交回宿主（edit 事件），宿主解析、校验、记草稿，
// 都在界面线程上。分别记「仅 edit 事件处理」与「事件加一帧」的 mean / p95 / max，以及 Go 堆在「进编辑态前 → 编辑中」的增量
// 与每个事件的平均分配量。不含页面一侧把全文序列化成 JSON 的耗时、跨进程传输与 CodeMirror 自己的内存（那些要真窗口，归 R 类）。
// 交替两份只差末尾一个字符的事件（序列化在循环之外），末字符用 ASCII 替换，这样汉字文档的字节数不会超过 MaxEditTextBytes。
//
// 耗时与机器有关，默认跳过：VIVA_FRAME_BUDGET=1 go test ./viva/screens/library -run TestDetailEditorScale -v -count=1。
// 正式读数用 BelowNormal 优先级串行跑 3 轮，日志记在 D:\Project\GuiZhi-bench\parity\m4-b070-editor-scale.txt。
func TestDetailEditorScale(t *testing.T) {
	if os.Getenv("VIVA_FRAME_BUDGET") == "" {
		t.Skip("帧耗时读数与机器有关，默认不跑：VIVA_FRAME_BUDGET=1 时运行")
	}
	cases := []struct {
		name  string
		units int
		wide  bool
		n     int
	}{
		{"1Mi_mixed", 1 << 20, false, 60},
		{"4Mi_mixed", 4 << 20, false, 30},
		{"8Mi_mixed", reader.MaxEditChars, false, 20},
		{"8Mi_cjk_24MiB_utf8", reader.MaxEditChars, true, 20},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { measureEditorScale(t, c.name, c.units, c.wide, c.n) })
	}
}

// measureEditorScale 在 200 条的库里把第一条的正文设成指定长度，选中它、握手、进编辑态，再量 n 次 edit 事件。
func measureEditorScale(t *testing.T, name string, units int, wide bool, n int) {
	doc := editorScaleDoc(units, wide)
	if got, over := utf16Exceeds(doc, units); over || (got != 0 && got != units) {
		t.Fatalf("夹具文档应恰好 %d 个码元，实际 %d", units, got)
	}
	if units > reader.MaxEditChars {
		t.Fatalf("夹具长度 %d 超过 MaxEditChars %d，进不了编辑态", units, reader.MaxEditChars)
	}
	entries := servicestest.GenerateEntries(200)
	rr := newReaderRig(t, func(r *rig) {
		r.svc.SeedEntries(entries)
		r.svc.SeedDetail(entries[0].ID, servicestest.DetailBody{Content: doc})
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

	heapBefore := frametime.HeapInUse()
	editName := locales.ThemedReadingReaderEditOriginal()
	if !rr.has(gfx.RoleButton, editName) {
		editName = locales.ThemedReadingReaderEditSummary()
	}
	start := time.Now()
	rr.click(gfx.RoleButton, editName)
	rr.frames(4)
	enter := time.Since(start)
	if v := lastView(t, p); v["kind"] != "editor" {
		t.Fatalf("点「编辑」之后应进编辑态（页面收到编辑视图），实际 %v；文档 %d 个码元", v["kind"], units)
	}
	t.Logf("%s 进编辑态（点击 + 4 帧，含全文的 JSON 往返）：%.1f ms", name, float64(enter)/float64(time.Millisecond))

	seq := p.last(t, "view")["seq"]
	runes := []rune(doc)
	base := string(runes[:len(runes)-1])
	var raw [2]string
	for i, tail := range []string{"a", "b"} {
		// 与页面（JSON.stringify 按插入顺序写字段）一致，t 在最前：只有这样的事件才按 MaxEditBytes 放宽，map 的键序会让大文档事件被当成普通事件丢掉。
		seqJSON, err := json.Marshal(seq)
		if err != nil {
			t.Fatal(err)
		}
		textJSON, err := json.Marshal(base + tail)
		if err != nil {
			t.Fatal(err)
		}
		raw[i] = `{"t":"edit","seq":` + string(seqJSON) + `,"text":` + string(textJSON) + `}`
	}
	posts := len(p.posts)

	// 先热身 2 次，再量：第一次要分配草稿，不属于稳态按键。
	for i := 0; i < 2; i++ {
		p.cfg.OnMessage(raw[i%2])
		if err := rr.session.Frame(); err != nil {
			t.Fatalf("热身帧失败：%v", err)
		}
	}
	var m0, m1 runtime.MemStats
	runtime.ReadMemStats(&m0)
	event, err := frametime.Measure(n, func(i int) error {
		p.cfg.OnMessage(raw[i%2])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&m1)
	perEvent := (m1.TotalAlloc - m0.TotalAlloc) / uint64(n)
	withFrame, err := frametime.Measure(n, func(i int) error {
		p.cfg.OnMessage(raw[i%2])
		return rr.session.Frame()
	})
	if err != nil {
		t.Fatalf("按键帧失败：%v", err)
	}
	// 读数只有在事件真的被宿主接受时才有意义：草稿必须是最后一个事件带来的全文（否则事件在解析前就被丢了，耗时是假的）。
	d, ok := rr.editDraftOf(entries[0].ID, sourceBody)
	wantTail := []string{"a", "b"}[(n-1)%2]
	if !ok || len(d.text) != len(base)+1 || !strings.HasSuffix(d.text, wantTail) {
		t.Fatalf("宿主没有接受 edit 事件：草稿存在=%v 长度 %d（期望 %d）", ok, len(d.text), len(base)+1)
	}
	heapAfter := frametime.HeapInUse()
	jsonMiB := float64(len(raw[0])) / (1 << 20)
	t.Logf("%s edit 事件 JSON %.2f MiB；仅宿主处理 edit 事件：%s；每事件分配 %.2f MiB", name, jsonMiB, event, float64(perEvent)/(1<<20))
	t.Logf("%s edit 事件加一帧：%s", name, withFrame)
	t.Logf("%s Go 堆（GC 后存活）：进编辑态前 %.1f MiB，编辑中 %.1f MiB，增量 %.1f MiB", name,
		float64(heapBefore)/(1<<20), float64(heapAfter)/(1<<20), (float64(heapAfter)-float64(heapBefore))/(1<<20))
	if got := len(p.posts) - posts; got != 0 {
		t.Errorf("页面自己报上来的改动不该再被整篇回灌：期间又发给页面 %d 条消息", got)
	}
}
