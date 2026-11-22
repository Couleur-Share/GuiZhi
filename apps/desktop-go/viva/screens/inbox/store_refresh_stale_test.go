package inbox

import (
	"context"
	"errors"
	"testing"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
)

// 本文件补 M3-1 变异检查（m31）暴露的缺口：刷新代次保护此前只测了主列表；Wiki 候选与语义索引两块各自也要丢弃过时的结果
// （成功的旧结果不能盖掉新结果，失败的旧结果不能留下局部错误条、不能把聚合行清成 0、不能写日志）。
// 做法与 TestStaleRefreshDoesNotOverwriteNewerResult 相同：服务替身的读取类方法在收到请求时就取下种子快照、再等闸门，
// 所以被卡住的旧请求放行后返回的是「发出时」的数据，正好构造「旧请求晚到」。

// staleSection 描述一块独立刷新的数据：怎么给它种旧值、新值，怎么从快照里读出来。
type staleSection struct {
	name    string // 被卡住的服务方法
	seedOld func(*servicestest.Inbox)
	seedNew func(*servicestest.Inbox)
	// count 读出这块数据在快照里的数字：聚合行计数
	count func(View) int
	// section 读出这块的局部错误
	section func(View) string
	// detail 读出界面持有的原始数据（Wiki 是候选，语义没有），0 表示不检查
	detail            func(View) int
	wantNew           int
	aggregateKindWant string
}

func staleSections() []staleSection {
	return []staleSection{
		{
			name:    "WikiCandidates",
			seedOld: func(s *servicestest.Inbox) { s.SetWikiCandidates(services.InboxWikiCandidates{Ready: 2}) },
			seedNew: func(s *servicestest.Inbox) { s.SetWikiCandidates(services.InboxWikiCandidates{Ready: 5}) },
			count:   func(v View) int { return v.Counts.WikiPending },
			section: func(v View) string { return v.SectionErrors.Wiki },
			detail: func(v View) int {
				if v.Wiki == nil {
					return -1
				}
				return v.Wiki.Ready
			},
			wantNew: 5, aggregateKindWant: services.InboxKindWikiPending,
		},
		{
			name: "SemanticStatus",
			seedOld: func(s *servicestest.Inbox) {
				s.SetSemanticStatus(services.InboxSemanticStatus{Configured: true, EligibleItems: 10, IndexedItems: 8})
			},
			seedNew: func(s *servicestest.Inbox) {
				s.SetSemanticStatus(services.InboxSemanticStatus{Configured: true, EligibleItems: 10, IndexedItems: 4})
			},
			count:   func(v View) int { return v.Counts.SemanticPending },
			section: func(v View) string { return v.SectionErrors.Semantic },
			wantNew: 6, aggregateKindWant: services.InboxKindSemanticPending,
		},
	}
}

// aggregateCount 是快照里某个聚合行的计数，没有这一行返回 -1。
func aggregateCount(v View, kind string) int {
	for _, item := range v.Items {
		if item.Kind == kind {
			return item.Count
		}
	}
	return -1
}

// startStaleRefresh 在后台发起第一轮刷新，并等到被卡住的那块请求发出；返回放行函数与「第一轮结束」的信号。
func startStaleRefresh(t *testing.T, r *rig, method string) (release func(), done <-chan struct{}) {
	t.Helper()
	release = r.svc.Hold(method)
	finished := make(chan struct{})
	go func() {
		r.store.Refresh(context.Background())
		close(finished)
	}()
	waitFor(t, "第一轮的 "+method+" 请求发出", func() bool { return r.svc.CallCount(method) == 1 })
	return release, finished
}

// 旧一轮的 Wiki 候选 / 语义状态晚到，不能覆盖新一轮已落地的结果。
func TestStaleSectionRefreshDoesNotOverwriteNewerResult(t *testing.T) {
	for _, sec := range staleSections() {
		t.Run(sec.name, func(t *testing.T) {
			r := newRig(t)
			r.useGoroutines()
			sec.seedOld(r.svc)
			release, firstDone := startStaleRefresh(t, r, sec.name)

			sec.seedNew(r.svc)
			r.store.Refresh(context.Background())
			v := r.view()
			if got := sec.count(v); got != sec.wantNew {
				t.Fatalf("第二轮应已落地：计数 %d，期望 %d", got, sec.wantNew)
			}

			release()
			waitDone(t, "第一轮结束", firstDone)
			v = r.view()
			if got := sec.count(v); got != sec.wantNew {
				t.Fatalf("旧一轮晚到的结果不应覆盖新结果：计数 %d，期望 %d", got, sec.wantNew)
			}
			if got := aggregateCount(v, sec.aggregateKindWant); got != sec.wantNew {
				t.Fatalf("旧一轮晚到之后聚合行的计数应仍是新值：%d，期望 %d", got, sec.wantNew)
			}
			if sec.detail != nil {
				if got := sec.detail(v); got != sec.wantNew {
					t.Fatalf("旧一轮晚到之后界面持有的原始数据应仍是新值：%d，期望 %d", got, sec.wantNew)
				}
			}
		})
	}
}

// 旧一轮的失败同样不能污染新一轮：不出局部错误条、不把聚合行清成 0、不写错误日志。
func TestStaleSectionRefreshFailureIsIgnored(t *testing.T) {
	for _, sec := range staleSections() {
		t.Run(sec.name, func(t *testing.T) {
			r := newRig(t)
			r.useGoroutines()
			r.svc.FailOn(sec.name, errors.New("旧请求失败"))
			release, firstDone := startStaleRefresh(t, r, sec.name)

			r.svc.FailOn(sec.name, nil) // 失败在请求发出时就已定下；之后的新一轮读取是成功的
			sec.seedNew(r.svc)
			r.store.Refresh(context.Background())
			if got := sec.count(r.view()); got != sec.wantNew {
				t.Fatalf("第二轮应已落地：计数 %d，期望 %d", got, sec.wantNew)
			}

			release()
			waitDone(t, "第一轮结束", firstDone)
			v := r.view()
			if msg := sec.section(v); msg != "" {
				t.Fatalf("旧请求的失败不应落成局部错误条：%q", msg)
			}
			if got := sec.count(v); got != sec.wantNew {
				t.Fatalf("旧请求的失败不应把计数清掉：%d，期望 %d", got, sec.wantNew)
			}
			if got := aggregateCount(v, sec.aggregateKindWant); got != sec.wantNew {
				t.Fatalf("旧请求的失败不应移除聚合行：%d，期望 %d", got, sec.wantNew)
			}
			if logs := r.log.entries(); len(logs) != 0 {
				t.Fatalf("过时的失败不应写日志：%v", logs)
			}
		})
	}
}
