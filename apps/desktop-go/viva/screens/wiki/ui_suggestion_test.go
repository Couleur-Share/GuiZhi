package wiki

import (
	"errors"
	"strings"
	"testing"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/locales"
)

// 更新建议与旧版覆盖提示（L4）。

func uiSuggestion(token, body string) *services.WikiSuggestion {
	return &services.WikiSuggestion{
		Draft:  services.WikiSuggestionDraft{Title: "Beta", Summary: "new summary", Body: body, Kind: services.WikiKindTopic},
		Model:  "m",
		Reason: "sources changed",
		Token:  token,
	}
}

func TestUILegacyPageOffersUpgrade(t *testing.T) {
	r := newUIRig(t, withSeed, func(r *uiRig) {
		r.svc.PutPage(services.WikiPage{ID: "old", Title: "Old", Body: "legacy body", PromptVersion: "v0-legacy"})
		r.svc.SetPreview(previewOf(manyReady(1)...))
	})
	r.clickButton("Alpha")
	if r.hasText(locales.WikiLegacyCoverage()) || r.hasButton(locales.WikiUpgradeSources()) {
		t.Fatal("当前版本编译的页面不该有旧版提示")
	}
	r.clickButton("Old")
	if !strings.Contains(r.allText(), locales.WikiLegacyCoverage()) || !r.hasButton(locales.WikiUpgradeSources()) {
		t.Fatalf("旧版页面应有覆盖提示与升级入口：%q", r.session.Texts())
	}
	r.clickButton(locales.WikiUpgradeSources())
	if !r.store.Compile().View().Open {
		t.Fatal("「选择资料升级」应打开编译对话框")
	}
}

func TestUISuggestionPanelExpandsAndAccepts(t *testing.T) {
	r := newUIRig(t, withSeed)
	r.svc.PutSuggestion("beta", uiSuggestion("t1", "suggested body"))
	r.clickButton("Beta")
	if !r.hasButton("sources changed") {
		t.Fatalf("有建议应显示折叠面板，摘要是建议的原因：%q", r.session.Texts())
	}
	if r.hasButton(locales.WikiSuggestionAccept()) {
		t.Fatal("面板默认收起，不该有采用按钮")
	}
	r.clickButton("sources changed")
	if !strings.Contains(r.allText(), locales.WikiSuggestionHint()) || !r.hasText("suggested body") || !r.hasText("new summary") {
		t.Fatalf("展开后应有说明、摘要与建议正文：%q", r.session.Texts())
	}
	r.clickButton(locales.WikiSuggestionAccept())
	if got := r.svc.Accepts(); len(got) != 1 || got[0].PageID != "beta" || got[0].Token != "t1" {
		t.Fatalf("应带着令牌采用建议：%+v", got)
	}
	if r.hasButton("sources changed") {
		t.Error("采用之后建议面板应消失")
	}
	if !r.hasText("suggested body") {
		t.Errorf("采用后正文应是建议内容：%q", r.session.Texts())
	}
}

func TestUISuggestionStaleTokenAsksToRecheck(t *testing.T) {
	r := newUIRig(t, withSeed)
	r.svc.PutSuggestion("beta", uiSuggestion("t1", "first draft"))
	r.clickButton("Beta")
	r.clickButton("sources changed")
	// 展开期间后台又生成了新建议：令牌换了。
	r.svc.PutSuggestion("beta", uiSuggestion("t2", "second draft"))
	r.clickButton(locales.WikiSuggestionAccept())
	if !strings.Contains(r.allText(), locales.NativeWikiSuggestionStale()) {
		t.Fatalf("令牌失效应提示重新核对：%q", r.session.Texts())
	}
	if sv := r.store.Suggestion().View(); sv.Suggestion == nil || sv.Suggestion.Token != "t2" {
		t.Fatalf("应自动重读到新建议：%+v", sv)
	}
	if !r.hasButton("sources changed") {
		t.Error("新建议应继续留在面板里供核对")
	}
}

func TestUISuggestionLoadFailureShowsReason(t *testing.T) {
	r := newUIRig(t, withSeed, func(r *uiRig) { r.svc.Fail(servicestest.WikiOpSuggestion, errors.New("suggestion boom")) })
	r.clickButton("Beta")
	if !r.hasText(locales.WikiSuggestionLoadFailed()) || !r.hasText("suggestion boom") {
		t.Fatalf("读取失败应有固定提示与可复制的原因：%q", r.session.Texts())
	}
}

func TestUISuggestionAcceptFailureKeepsSuggestion(t *testing.T) {
	r := newUIRig(t, withSeed)
	r.svc.PutSuggestion("beta", uiSuggestion("t1", "draft"))
	r.clickButton("Beta")
	r.clickButton("sources changed")
	r.svc.Fail(servicestest.WikiOpAccept, errors.New("accept boom"))
	r.clickButton(locales.WikiSuggestionAccept())
	if !r.hasText("accept boom") {
		t.Fatalf("采用失败应显示原因：%q", r.session.Texts())
	}
	if !r.hasButton(locales.WikiSuggestionAccept()) {
		t.Error("失败后建议仍在，可以重试")
	}
}
