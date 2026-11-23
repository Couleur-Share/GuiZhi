package themed

import (
	"errors"
	"testing"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
)

type modeRig struct {
	r *rig
	m *ModeStore
}

func newModeRig(t *testing.T) *modeRig {
	r := newRig(t)
	m := NewModeStore(r.svc, r.guard, func() {})
	m.Spawn = runNow
	return &modeRig{r: r, m: m}
}

func TestSourceKindOf(t *testing.T) {
	if SourceKindOf(TabSummary) != services.ThemedKindSummary || SourceKindOf(TabBody) != services.ThemedKindBody || SourceKindOf("notes") != services.ThemedKindBody {
		t.Fatal("讨论总结用 summary，其余一律是正文")
	}
}

func TestModeDefaultsToAIPageWhenItExists(t *testing.T) {
	g := newModeRig(t)
	g.r.svc.PutPage(reqA, services.ThemedPage{})
	g.m.Open(reqA.ItemID, TabBody, true)
	if v := g.m.View(); !v.Active || !v.Available || v.Editing || v.SourceKind != services.ThemedKindBody {
		t.Fatalf("有成品时默认阅读 AI 页: %+v", v)
	}
	g.m.Open(reqB.ItemID, TabBody, true)
	if v := g.m.View(); v.Active || !v.Available {
		t.Fatalf("没有成品时停在原文: %+v", v)
	}
}

func TestModeUnavailableTabsAndDisabledFeature(t *testing.T) {
	g := newModeRig(t)
	g.r.svc.PutPage(reqA, services.ThemedPage{})
	g.m.Open(reqA.ItemID, "notes", true)
	if v := g.m.View(); v.Available || v.Active {
		t.Fatalf("其他标签不可用: %+v", v)
	}
	g.m.Open(reqA.ItemID, TabBody, false)
	if v := g.m.View(); v.Available || v.Active {
		t.Fatalf("功能未启用不可用: %+v", v)
	}
	if n := len(g.r.svc.Calls().States); n != 0 {
		t.Fatalf("不可用时不探测: %d", n)
	}
}

func TestModeSameKeyKeepsUserChoice(t *testing.T) {
	g := newModeRig(t)
	g.r.svc.PutPage(reqA, services.ThemedPage{})
	g.m.Open(reqA.ItemID, TabBody, true)
	g.m.Select(false)
	g.m.Open(reqA.ItemID, TabBody, true) // 重渲染：内容不变
	if g.m.View().Active {
		t.Fatal("内容没变不应重新探测并覆盖用户的选择")
	}
	if n := len(g.r.svc.Calls().States); n != 1 {
		t.Fatalf("只探测一次: %d", n)
	}
	g.m.Toggle()
	if !g.m.View().Active {
		t.Fatal("切换回 AI 阅读")
	}
	g.m.Open(reqA.ItemID, TabSummary, true) // 换标签等于换内容
	if v := g.m.View(); v.Active || v.SourceKind != services.ThemedKindSummary {
		t.Fatalf("换来源回到原文并重新探测: %+v", v)
	}
}

func TestModeUserChoiceDuringProbeWins(t *testing.T) {
	g := newModeRig(t)
	g.r.svc.PutPage(reqA, services.ThemedPage{})
	g.m.Spawn = func(fn func()) { go fn() }
	release, arrived := g.r.svc.Hold(servicestest.ThemedOpState)
	g.m.Open(reqA.ItemID, TabBody, true)
	<-arrived
	g.m.Select(false) // 探测期间用户主动选了原文
	release()
	waitFor(t, "探测收尾", func() bool { return len(g.r.svc.Calls().States) == 1 })
	// 给收尾 goroutine 一点时间；若它错误地覆盖，会在这里体现
	waitStable(t, func() bool { return !g.m.View().Active })
}

func TestModeProbeIgnoredAfterSwitchAndWhileEditing(t *testing.T) {
	g := newModeRig(t)
	g.r.svc.PutPage(reqA, services.ThemedPage{})
	g.m.Spawn = func(fn func()) { go fn() }
	release, arrived := g.r.svc.Hold(servicestest.ThemedOpState)
	g.m.Open(reqA.ItemID, TabBody, true)
	<-arrived
	g.m.Open(reqB.ItemID, TabBody, true) // 切到没有成品的内容
	release()
	waitStable(t, func() bool { v := g.m.View(); return v.ItemID == reqB.ItemID && !v.Active })

	g.m.Spawn = runNow
	g.m.StartEditing()
	if v := g.m.View(); !v.Editing || v.Active {
		t.Fatalf("编辑中: %+v", v)
	}
	g.m.FinishEditing()
	if v := g.m.View(); v.Editing || v.Active {
		t.Fatalf("结束编辑回到原文: %+v", v)
	}
}

func TestModeEditingHidesAIPage(t *testing.T) {
	g := newModeRig(t)
	g.r.svc.PutPage(reqA, services.ThemedPage{})
	g.m.Open(reqA.ItemID, TabBody, true)
	g.m.StartEditing()
	if v := g.m.View(); v.Active || !v.Editing {
		t.Fatalf("%+v", v)
	}
}

func TestModeProbeFailureStaysOnOriginalAndLogs(t *testing.T) {
	g := newModeRig(t)
	g.r.svc.Fail(servicestest.ThemedOpState, errors.New("db locked"))
	g.m.Open(reqA.ItemID, TabBody, true)
	if g.m.View().Active {
		t.Fatal("探测失败停在原文")
	}
	if len(g.r.log.entries()) != 1 {
		t.Fatalf("应留痕: %v", g.r.log.entries())
	}
	for _, q := range g.r.toasts() {
		if q.Kind == kit.ToastError {
			t.Fatal("默认进入模式的探测失败不弹提示，完整面板负责展示可重试的错误")
		}
	}
}
