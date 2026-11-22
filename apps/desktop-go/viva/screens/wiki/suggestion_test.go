package wiki

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
)

func suggestionFor(token, body string) *services.WikiSuggestion {
	return &services.WikiSuggestion{
		Draft:     services.WikiSuggestionDraft{Title: "Beta", Summary: "new summary", Body: body, Kind: services.WikiKindTopic},
		Model:     "m",
		SourceIDs: []string{"s1", "s2"},
		Reason:    "sources changed",
		Token:     token,
	}
}

func TestSuggestionFollowsSelectedPage(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.svc.PutSuggestion("beta", suggestionFor("t1", "suggested body"))
	r.ready()
	if s := r.sview(); s.PageID != "" || s.Suggestion != nil {
		t.Fatalf("没选中页：%+v", s)
	}
	r.store.SelectPage("beta")
	s := r.sview()
	if s.PageID != "beta" || s.Suggestion == nil || s.Suggestion.Token != "t1" || s.Loading || s.Error != "" || s.Stale {
		t.Fatalf("%+v", s)
	}
	r.store.SelectPage("alpha")
	if s := r.sview(); s.PageID != "alpha" || s.Suggestion != nil {
		t.Fatalf("切到没有建议的页：旧建议立即消失 %+v", s)
	}
}

func TestSuggestionNotRereadWhilePageUnchanged(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	r.store.SelectPage("beta")
	r.store.SelectPage("beta")
	r.store.Refresh()
	reads := 0
	for _, g := range r.svc.Gets() {
		if g == "beta" {
			reads++
		}
	}
	if reads != 3 {
		t.Fatalf("前置：页面被读了 %d 次", reads)
	}
	r.svc.PutSuggestion("beta", suggestionFor("late", "x"))
	r.store.SelectPage("beta") // 页面更新时间没变：不重读建议（避免每次刷新都打一次请求）
	if r.sview().Suggestion != nil {
		t.Fatal("页面未更新不重读")
	}
	r.compile().Open()
	r.store.Suggestion().Reload() // 一轮编译结束后显式重读
	if s := r.sview(); s.Suggestion == nil || s.Suggestion.Token != "late" {
		t.Fatalf("%+v", s)
	}
}

func TestSuggestionRereadWhenPageUpdated(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.ready()
	r.store.SelectPage("beta")
	r.svc.PutSuggestion("beta", suggestionFor("fresh", "x"))
	if _, err := r.svc.UpdatePage(t.Context(), services.WikiUpdatePageInput{PageID: "beta", Body: "edited elsewhere"}); err != nil {
		t.Fatal(err)
	}
	r.store.Refresh()
	if s := r.sview(); s.Suggestion == nil || s.Suggestion.Token != "fresh" {
		t.Fatalf("页面更新时间变了要重读建议：%+v", s)
	}
}

func TestSuggestionReloadWithoutPageIsNoop(t *testing.T) {
	r := newRig(t)
	r.store.Suggestion().Reload()
	if s := r.sview(); s.PageID != "" || s.Loading {
		t.Fatalf("%+v", s)
	}
}

func TestSuggestionReadFailureThenRetryOnNextFollow(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.svc.PutSuggestion("beta", suggestionFor("t1", "b"))
	r.ready()
	r.svc.Fail(servicestest.WikiOpSuggestion, errors.New("suggestion read failed"))
	r.store.SelectPage("beta")
	s := r.sview()
	if s.Error != "suggestion read failed" || s.Suggestion != nil || s.Loading {
		t.Fatalf("%+v", s)
	}
	if v := r.view(); v.Detail == nil || v.PageError != "" {
		t.Fatal("建议读取失败不影响页面详情")
	}
	r.wantNoToast()
	if got := r.log.entries(); !slices.Contains(got, "wiki.suggestion|suggestion read failed") {
		t.Fatalf("%v", got)
	}
	r.svc.Fail(servicestest.WikiOpSuggestion, nil)
	r.store.SelectPage("beta") // 失败的读取不算数：同一页再选会重试
	if s := r.sview(); s.Error != "" || s.Suggestion == nil {
		t.Fatalf("%+v", s)
	}
}

func TestSuggestionAcceptSuccess(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.svc.PutSuggestion("beta", suggestionFor("t1", "accepted body with [[Alpha]]"))
	r.ready()
	r.store.SelectPage("beta")
	r.store.BeginEdit()
	r.store.SetDraft("half-typed edit")
	if !r.store.Suggestion().Accept() {
		t.Fatalf("应成功：%+v", r.sview())
	}
	s := r.sview()
	if s.Suggestion != nil || s.Error != "" || s.Stale {
		t.Fatalf("%+v", s)
	}
	v := r.view()
	if v.Detail.Page.Body != "accepted body with [[Alpha]]" || v.Editing || v.Draft != "" || len(v.Revisions) != 1 {
		t.Fatalf("采用后重读页面、丢弃基于旧正文的草稿、存一份历史：%+v", v.Detail.Page)
	}
	if got := r.svc.Accepts(); len(got) != 1 || got[0].Token != "t1" || got[0].PageID != "beta" {
		t.Fatalf("%+v", got)
	}
	r.wantNoToast()
}

func TestSuggestionAcceptStaleReloadsAndFlags(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.svc.PutSuggestion("beta", suggestionFor("old", "old body"))
	r.ready()
	r.store.SelectPage("beta")
	r.svc.PutSuggestion("beta", suggestionFor("new", "newer body")) // 后台又换了一份
	if r.store.Suggestion().Accept() {
		t.Fatal("令牌失效不能采用")
	}
	s := r.sview()
	if !s.Stale || s.Error != "" || s.Suggestion == nil || s.Suggestion.Token != "new" {
		t.Fatalf("令牌失效：自动重读并提示重新核对，不当作故障：%+v", s)
	}
	if p, _ := r.svc.Page("beta"); p.Body != "body of beta" {
		t.Fatal("页面不变")
	}
	r.wantNoToast()
	if !r.store.Suggestion().Accept() {
		t.Fatal("核对后重新采用")
	}
	if s := r.sview(); s.Stale || s.Suggestion != nil {
		t.Fatalf("采用成功清掉过期标记：%+v", s)
	}
}

func TestSuggestionAcceptStaleWhenSuggestionVanished(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.svc.PutSuggestion("beta", suggestionFor("t", "b"))
	r.ready()
	r.store.SelectPage("beta")
	r.svc.PutSuggestion("beta", nil)
	if r.store.Suggestion().Accept() {
		t.Fatal("建议已被清掉")
	}
	if s := r.sview(); !s.Stale || s.Suggestion != nil {
		t.Fatalf("%+v", s)
	}
}

func TestSuggestionAcceptErrorKeepsSuggestionAndRetries(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.svc.PutSuggestion("beta", suggestionFor("t1", "b"))
	r.ready()
	r.store.SelectPage("beta")
	r.svc.Fail(servicestest.WikiOpAccept, errors.New("write refused"))
	if r.store.Suggestion().Accept() {
		t.Fatal("应失败")
	}
	s := r.sview()
	if s.Error != "write refused" || s.Suggestion == nil || s.Stale {
		t.Fatalf("失败保留建议以便重试：%+v", s)
	}
	if got := r.log.entries(); !slices.Contains(got, "wiki.acceptSuggestion|write refused") {
		t.Fatalf("%v", got)
	}
	r.svc.Fail(servicestest.WikiOpAccept, nil)
	if !r.store.Suggestion().Accept() {
		t.Fatal("可重试")
	}
	if s := r.sview(); s.Error != "" || s.Suggestion != nil {
		t.Fatalf("成功后错误清除：%+v", s)
	}
}

func TestSuggestionAcceptIgnoredWithoutSuggestionOrWhileLoading(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.svc.PutSuggestion("beta", suggestionFor("t", "b"))
	r.ready()
	if r.store.Suggestion().Accept() {
		t.Fatal("没有选中页")
	}
	r.store.SelectPage("alpha")
	if r.store.Suggestion().Accept() || len(r.svc.Accepts()) != 0 {
		t.Fatal("没有建议不发请求")
	}
	r.useGoroutines()
	release, arrived := r.svc.Hold(servicestest.WikiOpSuggestion)
	wait := inBackground(func() { r.store.SelectPage("beta") })
	<-arrived
	if r.store.Suggestion().Accept() {
		t.Fatal("读取中不采用")
	}
	release()
	wait()
	if len(r.svc.Accepts()) != 0 {
		t.Fatal("读取中不发请求")
	}
}

func TestSuggestionClearedOnDeleteOfFollowedPageOnly(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.svc.PutSuggestion("beta", suggestionFor("t", "b"))
	r.ready()
	r.store.SelectPage("beta")
	r.store.Suggestion().clearIf("alpha")
	if r.sview().Suggestion == nil {
		t.Fatal("删别的页不影响")
	}
	r.store.DeletePage("beta")
	if s := r.sview(); s.PageID != "" || s.Suggestion != nil {
		t.Fatalf("%+v", s)
	}
}

func TestSuggestionStaleReadDropped(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.svc.PutSuggestion("beta", suggestionFor("beta-t", "b"))
	r.svc.PutSuggestion("gamma", suggestionFor("gamma-t", "g"))
	r.ready()
	release, arrived := r.svc.Hold(servicestest.WikiOpSuggestion)
	waitBeta := inBackground(func() { r.store.SelectPage("beta") })
	<-arrived
	r.store.SelectPage("gamma")
	release()
	waitBeta()
	s := r.sview()
	if s.PageID != "gamma" || s.Suggestion == nil || s.Suggestion.Token != "gamma-t" || s.Loading {
		t.Fatalf("先发的读取迟到不能覆盖 Gamma：%+v", s)
	}
}

func TestLegacyCoverage(t *testing.T) {
	if LegacyCoverage(services.WikiCompilerVersion) {
		t.Fatal("当前版本不是旧版")
	}
	for _, v := range []string{"", "fulltext-v1", "other"} {
		if !LegacyCoverage(v) {
			t.Fatalf("%q 应视为旧版", v)
		}
	}
	if !strings.Contains(services.WikiCompilerVersion, "fulltext") {
		t.Fatal("编译器版本标识")
	}
}

func TestSuggestionConcurrentSelectAndAccept(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.svc.PutSuggestion("beta", suggestionFor("t", "b"))
	r.ready()
	r.useGoroutines()
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.store.SelectPage([]string{"alpha", "beta", "gamma"}[i%3])
			_ = r.sview()
			r.store.Suggestion().Reload()
			r.store.Suggestion().Accept()
		}()
	}
	wg.Wait()
	r.store.SelectPage("gamma")
	if s := r.sview(); s.PageID != "gamma" || s.Loading {
		t.Fatalf("%+v", s)
	}
}
