package ask

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/aiflow"
	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/internal/frametime"
	"guizhi-mygo-shell/viva/locales"
)

// TestAskFrameBudget 读出 AI 问答的帧耗时（口径同 viva/screens/capture 的 TestCaptureFrameBudget：无头会话的 Frame() 只记录绘制命令、
// 不栅格化，量的是界面线程的出帧成本；目标是单帧远低于 16 ms 预算）。
//
// 五个代表性场景加一个压力场景：
//   - ask_empty：没有任何历史会话的空态（对照组）；
//   - ask_typical_conversation：40 条历史会话 + 当前会话 8 轮问答，每轮带多处 [n] 引用与 3 个来源 chip；
//   - ask_streaming：回答进行中（卡在流式文本，停止钮、步骤与已推送的部分回答都在画面上）；
//   - ask_evidence_expanded：8 轮会话里展开了一个来源的证据摘录；
//   - ask_article_panel：围绕本文面板（知识库详情右侧栏）里 12 轮带来源、警告与模型标注的问答；
//   - ask_long_conversation（压力）：当前会话 30 轮问答——消息列表整页声明、没有窗口化，量的是上界。
//
// 量四个动作：稳态帧（画面不变，连出 300 帧）、悬停一步（指针在两个有悬停反馈的控件之间来回）、滚动一步（消息列表上下来回，
// 先核对滚动确实改变了画面）、栅格化加回读（上界）；再给出打开后的 Go 堆增量（含无头会话）。每组都给出 p50 与单帧最大值
// （Windows 上计时器粒度约 0.5～1 ms，小于它的帧 p50 会读成 0.00，看 mean 与 p95）。
//
// 耗时与机器有关，默认跳过：VIVA_FRAME_BUDGET=1 go test ./viva/screens/ask -run TestAskFrameBudget -v -count=1。
func TestAskFrameBudget(t *testing.T) {
	if os.Getenv("VIVA_FRAME_BUDGET") == "" {
		t.Skip("帧耗时读数与机器有关，默认不跑：VIVA_FRAME_BUDGET=1 时运行")
	}
	t.Run("ask_empty", func(t *testing.T) {
		measureAsk(t, "ask_empty", budgetSpec{
			open: func() *uiRig { return newUIRig(t) },
			a:    pointOf(gfx.RoleButton, locales.AskNewSession()), b: pointOf(gfx.RoleButton, locales.AskExample1()),
		})
	})
	t.Run("ask_typical_conversation", func(t *testing.T) {
		measureAsk(t, "ask_typical_conversation", budgetSpec{
			open: func() *uiRig { return newUIRig(t, withActive("long"), longConversation(t, "long", 8)) },
			a:    pointOf(gfx.RoleButton, locales.AskNewSession()), b: pointOf(gfx.RoleButton, budgetChip),
			scrollX: 760, scrollY: 400,
		})
	})
	t.Run("ask_streaming", func(t *testing.T) {
		gate := make(chan struct{})
		defer close(gate)
		measureAsk(t, "ask_streaming", budgetSpec{
			open: func() *uiRig {
				r := newUIRig(t, goroutines, func(r *uiRig) {
					r.svc.QueueAsk(servicestest.AskScript{
						Steps: []string{"检索知识库", "阅读来源"}, Texts: []string{strings.Repeat("流式输出的一部分回答。", 40)},
						Gate: gate, Answer: services.AskAnswer{Text: "done"},
					})
				})
				r.settle("首次加载结束", func() bool { return r.store.View().HasLoaded })
				r.screen.setDraft("一个正在回答的问题")
				r.frames(2)
				r.clickButton(locales.AskSend())
				r.settle("回答进行中", func() bool { return r.store.View().IsRunning && r.contains("流式输出的一部分回答") })
				return r
			},
			a: pointOf(gfx.RoleButton, locales.AskNewSession()), b: pointOf(gfx.RoleButton, locales.AskStop()),
		})
	})
	t.Run("ask_evidence_expanded", func(t *testing.T) {
		measureAsk(t, "ask_evidence_expanded", budgetSpec{
			open: func() *uiRig {
				r := newUIRig(t, withActive("long"), longConversation(t, "long", 8))
				r.clickButton(budgetChip)
				if !r.has(gfx.RoleGroup, locales.AskEvidenceTitle()) {
					t.Fatalf("夹具应展开证据摘录：%q", r.session.Texts())
				}
				return r
			},
			a: pointOf(gfx.RoleButton, locales.AskNewSession()), b: pointOf(gfx.RoleButton, locales.AskEvidenceViewCurrent()),
			scrollX: 760, scrollY: 400,
		})
	})
	t.Run("ask_article_panel", func(t *testing.T) {
		measureAsk(t, "ask_article_panel", budgetSpec{
			open: func() *uiRig {
				msgs := make([]aiflow.ArticleMessage, 0, 12)
				for i := 0; i < 12; i++ {
					m := richTurn()
					m.ID = fmt.Sprintf("m%d", i)
					m.Question = fmt.Sprintf("问题 %d：这篇文章里关于并发模式是怎么说的？", i)
					m.Answer = strings.Repeat("回答里解释了并发模式的取舍，并引用来源。", 6)
					msgs = append(msgs, m)
				}
				return newArticleRig(t, withArticleSession("as1", msgs...), func(r *uiRig) {
					r.articleSpec.SessionID = "as1"
					r.articleSpec.Target = target("item-1")
				})
			},
			a: pointOf(gfx.RoleButton, locales.ArticleAskNew()), b: pointOf(gfx.RoleButton, locales.ArticleAskHistory()),
			scrollX: 200, scrollY: 400, // 模块隐藏时面板画在窗口最左边（x 0～400）
		})
	})
	t.Run("ask_long_conversation", func(t *testing.T) {
		measureAsk(t, "ask_long_conversation", budgetSpec{
			open: func() *uiRig { return newUIRig(t, withActive("long"), longConversation(t, "long", 30)) },
			a:    pointOf(gfx.RoleButton, locales.AskNewSession()), b: pointOf(gfx.RoleButton, budgetChip),
			scrollX: 760, scrollY: 400,
		})
	})
}

// budgetChip 是会话里来源 chip 的名字（每轮都用 source(1, "i1", …)）。
const budgetChip = "[1] title-i1"

// longConversation 在库里摆一个 rounds 轮问答的全局会话（再加 40 条历史会话让侧栏的列表也是满的）。
func longConversation(t *testing.T, id string, rounds int) func(*uiRig) {
	return func(r *uiRig) {
		msgs := make([]services.AskMessage, 0, rounds)
		for i := 0; i < rounds; i++ {
			m := done(fmt.Sprintf("m%d", i), fmt.Sprintf("问题 %d：知识库里关于并发模式有哪些记录？", i),
				strings.Repeat("并发模式的要点见 [1]，取舍与限制见 [2, 3]。", 5)+"\n\n- 第一点 [1]\n- 第二点 [2]\n- 第三点 [3]")
			m.Sources = []services.AskSource{source(1, "i1", "fp1"), source(2, "i2", "fp2"), source(3, "i3", "fp3")}
			msgs = append(msgs, m)
		}
		putSession(t, r.svc, id, msgs...)
		for i := 0; i < 40; i++ {
			putSession(t, r.svc, fmt.Sprintf("old-%02d", i), done(fmt.Sprintf("o%d", i), fmt.Sprintf("历史问题 %d", i), "答"))
		}
	}
}

// budgetSpec 描述一个场景：open 造出夹具（已经稳定），a、b 是悬停来回的两个控件，scrollX/Y 非零时量滚动一步。
type budgetSpec struct {
	open    func() *uiRig
	a, b    pointFn
	scrollX float32
	scrollY float32
}

type pointFn func(r *uiRig) (x, y float32, ok bool)

// pointOf 取角色加名称对应控件的中心点。
func pointOf(role gfx.Role, name string) pointFn {
	return func(r *uiRig) (float32, float32, bool) {
		n, ok := r.node(role, name)
		if !ok {
			return 0, 0, false
		}
		return n.Bounds.X + n.Bounds.Width/2, n.Bounds.Y + n.Bounds.Height/2, true
	}
}

func measureAsk(t *testing.T, name string, spec budgetSpec) {
	heapBefore := frametime.HeapInUse()
	r := spec.open()
	r.frames(4)
	t.Logf("%s 打开后 Go 堆增量（含无头会话）：%d KiB", name, (int64(frametime.HeapInUse())-int64(heapBefore))/1024)

	idle, err := frametime.Measure(300, func(int) error { return r.session.Frame() })
	if err != nil {
		t.Fatalf("稳态帧失败：%v", err)
	}
	t.Logf("%s 稳态帧（布局+记录）：%s", name, idle)

	ax, ay, okA := spec.a(r)
	bx, by, okB := spec.b(r)
	if !okA || !okB {
		t.Fatalf("找不到悬停目标（a=%v b=%v）；画面上有 %q", okA, okB, r.session.Texts())
	}
	if ax == bx && ay == by {
		t.Fatal("两个悬停点重合，悬停读数无效")
	}
	hover, err := frametime.Measure(200, func(i int) error {
		x, y := ax, ay
		if i%2 == 1 {
			x, y = bx, by
		}
		if err := r.session.Move(x, y); err != nil {
			return err
		}
		return r.session.Frame()
	})
	if err != nil {
		t.Fatalf("悬停帧失败：%v", err)
	}
	t.Logf("%s 悬停一步（移动指针+出帧）：%s", name, hover)

	if spec.scrollX != 0 || spec.scrollY != 0 {
		before := frametime.Fingerprint(r.session.Access())
		if err := r.session.Scroll(gfx.ScrollEvent{X: spec.scrollX, Y: spec.scrollY, DeltaY: -240, Precise: true}); err != nil {
			t.Fatal(err)
		}
		r.frames(2)
		if frametime.Fingerprint(r.session.Access()) == before {
			t.Fatal("滚动没有改变画面，滚动读数无效")
		}
		scroll, err := frametime.Measure(120, func(i int) error {
			dy := float32(120)
			if i%2 == 1 {
				dy = -120
			}
			if err := r.session.Scroll(gfx.ScrollEvent{X: spec.scrollX, Y: spec.scrollY, DeltaY: dy, Precise: true}); err != nil {
				return err
			}
			return r.session.Frame()
		})
		if err != nil {
			t.Fatalf("滚动帧失败：%v", err)
		}
		t.Logf("%s 滚动一步（滚轮+出帧）：%s", name, scroll)
	}

	raster, err := frametime.Measure(30, func(int) error {
		_, err := r.session.Image()
		return err
	})
	if err != nil {
		t.Fatalf("栅格化失败：%v", err)
	}
	t.Logf("%s 栅格化+回读（Image，上界）：%s", name, raster)
}
