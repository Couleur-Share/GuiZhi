package themed

import (
	"errors"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// openSetup 在没有页面的条目上点「生成阅读页」，打开生成设置。
func (r *uiRig) openSetup() {
	r.t.Helper()
	r.activate()
	r.clickButton(locales.ThemedReadingReaderCreate())
	if !r.hasButton(locales.ThemedReadingGenerate()) {
		r.t.Fatalf("设置对话框没有打开；画面上有 %q", r.session.Texts())
	}
}

func TestSetupEmptyStateOffersCreate(t *testing.T) {
	r := newUIRig(t)
	r.activate()
	if !r.hasText(locales.ThemedReadingReaderEmpty()) {
		t.Fatalf("没有页面时应显示空态；画面上有 %q", r.session.Texts())
	}
	if !r.hasButton(locales.ThemedReadingReaderCreate()) {
		t.Fatal("空态应提供「生成阅读页」")
	}
	if r.hasButton(locales.ThemedReadingGenerate()) {
		t.Fatal("设置对话框不该未经点击就打开")
	}
}

func TestSetupGenerateSubmitsDefaultOptions(t *testing.T) {
	r := newUIRig(t)
	r.openSetup()
	r.clickButton(locales.ThemedReadingGenerate())
	gens := r.svc.Calls().Generates
	if len(gens) != 1 {
		t.Fatalf("应发出 1 次生成，实际 %d", len(gens))
	}
	got := gens[0]
	if got.Req != reqA {
		t.Fatalf("请求 = %+v，想要 %+v", got.Req, reqA)
	}
	o := got.Opts
	if !o.GenerateImages || o.MaxImages < 1 || o.Research || !o.Enhanced() {
		t.Fatalf("默认选项不对：%+v", o)
	}
	if r.hasButton(locales.ThemedReadingGenerate()) {
		t.Fatal("生成发出后对话框应关闭")
	}
	if len(r.toasts()) != 0 {
		t.Fatalf("成功时不该有提示：%+v", r.toasts())
	}
}

func TestSetupCancelDoesNotGenerate(t *testing.T) {
	r := newUIRig(t)
	r.openSetup()
	r.clickButton(locales.CommonCancel())
	if r.hasButton(locales.ThemedReadingGenerate()) {
		t.Fatal("取消后对话框应关闭")
	}
	if n := len(r.svc.Calls().Generates); n != 0 {
		t.Fatalf("取消不该生成，实际 %d 次", n)
	}
}

func TestSetupResearchWithoutSearchBlocksGenerate(t *testing.T) {
	r := newUIRig(t)
	r.openSetup()
	if r.textHas(locales.ThemedReadingReaderSearchMissing()) {
		t.Fatal("没勾选联网查证时不该提示没配置搜索")
	}
	r.click(gfx.RoleCheckbox, locales.ThemedReadingReaderResearchOption())
	if !r.textHas(locales.ThemedReadingReaderSearchMissing()) {
		t.Fatalf("勾选联网查证但没配置搜索，应给出提示；画面上有 %q", r.session.Texts())
	}
	r.clickButton(locales.ArticleAskConfigure())
	if r.settingsOpened() != 1 {
		t.Fatalf("「前往配置」应跳到设置的 AI 分区，实际 %d 次", r.settingsOpened())
	}
	r.settle()
	if r.hasButton(locales.ThemedReadingGenerate()) {
		t.Fatal("跳去配置后设置对话框应关闭")
	}
	if n := len(r.svc.Calls().Generates); n != 0 {
		t.Fatalf("没配置搜索不该发出生成，实际 %d 次", n)
	}
}

func TestSetupResearchWithSearchSubmitsResearch(t *testing.T) {
	r := newUIRig(t)
	r.svc.SetSearch(services.ThemedSearchStatus{Configured: true, Provider: "tavily", ConfiguredProviders: []string{"tavily"}})
	r.openSetup()
	r.click(gfx.RoleCheckbox, locales.ThemedReadingReaderResearchOption())
	if r.textHas(locales.ThemedReadingReaderSearchMissing()) {
		t.Fatal("搜索已配置时不该提示缺失")
	}
	r.clickButton(locales.ThemedReadingGenerate())
	gens := r.svc.Calls().Generates
	if len(gens) != 1 || !gens[0].Opts.Research {
		t.Fatalf("应带联网查证发出生成：%+v", gens)
	}
	if gens[0].Opts.ResearchDepth == "" {
		t.Fatal("联网查证应带查证深度")
	}
}

func TestSetupWithoutImageModelIsTextOnly(t *testing.T) {
	r := newUIRig(t)
	r.svc.SetModels("text-model", "")
	r.openSetup()
	if !r.textHas(locales.ThemedReadingImageOptionalHint()) {
		t.Fatalf("没配生图模型时应提示可以只排版；画面上有 %q", r.session.Texts())
	}
	r.clickButton(locales.ThemedReadingGenerate())
	gens := r.svc.Calls().Generates
	if len(gens) != 1 || gens[0].Opts.GenerateImages {
		t.Fatalf("没有生图模型应只排版：%+v", gens)
	}
}

func TestSetupWithoutTextModelCannotGenerate(t *testing.T) {
	r := newUIRig(t)
	r.svc.SetModels("", "")
	r.openSetup()
	if !r.textHas(locales.ThemedReadingConfigureHint()) {
		t.Fatalf("没有文本模型应提示去配置；画面上有 %q", r.session.Texts())
	}
	if n, ok := r.node(gfx.RoleButton, locales.ThemedReadingGenerate()); !ok || !n.Disabled {
		t.Fatalf("没有文本模型时「生成」应禁用：%+v ok=%v", n, ok)
	}
	r.clickButton(locales.ThemedReadingConfigure())
	if r.settingsOpened() != 1 {
		t.Fatalf("「配置模型」应跳到设置的 AI 分区，实际 %d 次", r.settingsOpened())
	}
	if n := len(r.svc.Calls().Generates); n != 0 {
		t.Fatalf("不该生成，实际 %d 次", n)
	}
}

func TestSetupImagesOffSubmitsTextOnly(t *testing.T) {
	r := newUIRig(t)
	r.openSetup()
	r.click(gfx.RoleCheckbox, locales.ThemedReadingGenerateImages())
	if !r.textHas(locales.ThemedReadingNoImageCost()) {
		t.Fatalf("关掉生图应说明不产生生图费用；画面上有 %q", r.session.Texts())
	}
	r.clickButton(locales.ThemedReadingGenerate())
	gens := r.svc.Calls().Generates
	if len(gens) != 1 || gens[0].Opts.GenerateImages {
		t.Fatalf("应不生图：%+v", gens)
	}
}

func TestSetupStyleTextIsSubmitted(t *testing.T) {
	r := newUIRig(t)
	r.openSetup()
	r.click(gfx.RoleTextField, locales.ThemedReadingStyle())
	r.typeText("short and clear")
	r.clickButton(locales.ThemedReadingGenerate())
	gens := r.svc.Calls().Generates
	if len(gens) != 1 || !strings.Contains(gens[0].Opts.Style, "short and clear") {
		t.Fatalf("风格说明应随请求发出：%+v", gens)
	}
}

func TestSetupGenerateFailureKeepsDialogAndToasts(t *testing.T) {
	r := newUIRig(t)
	r.svc.Fail("generate", errors.New("quota exhausted"))
	r.openSetup()
	r.clickButton(locales.ThemedReadingGenerate())
	if !r.hasButton(locales.ThemedReadingGenerate()) {
		t.Fatal("生成失败后对话框应保留，便于重试")
	}
	if got := r.lastToast(); !strings.Contains(got.Detail, "quota exhausted") {
		t.Fatalf("失败提示应带可复制的原因：%+v", got)
	}
	r.svc.Fail("generate", nil)
	r.clickButton(locales.ThemedReadingGenerate())
	if r.hasButton(locales.ThemedReadingGenerate()) {
		t.Fatal("重试成功后对话框应关闭")
	}
	if n := len(r.svc.Calls().Generates); n != 2 {
		t.Fatalf("应共调用 2 次（1 失败 + 1 成功），实际 %d", n)
	}
}

// openTool 打开「阅读页设置」菜单并点其中一项。
func (r *uiRig) openTool(label string) {
	r.t.Helper()
	r.clickButton(locales.ThemedReadingReaderSettings())
	r.click(gfx.RoleMenuItem, label)
}

func TestSetupAdjustRedesignsFromCurrentPage(t *testing.T) {
	r := newUIRig(t)
	r.putPage(func(p *services.ThemedPage) {
		p.Options = services.ThemedOptions{Style: "calm tone", MaxImages: 2}
	})
	r.activate()
	r.openTool(locales.ThemedReadingReaderAdjust())
	if !r.hasButton(locales.ThemedReadingGenerate()) {
		t.Fatalf("应打开调整对话框；画面上有 %q", r.session.Texts())
	}
	if r.textHas(locales.ThemedReadingReaderResearchOption()) {
		t.Fatal("「调整设计」不涉及原文，不该出现联网查证")
	}
	r.clickButton(locales.ThemedReadingGenerate())
	gens := r.svc.Calls().Generates
	if len(gens) != 1 {
		t.Fatalf("应发出 1 次生成，实际 %d", len(gens))
	}
	o := gens[0].Opts
	if o.Action != services.ThemedActionRedesign || !o.FromCurrent || o.Style != "calm tone" {
		t.Fatalf("调整设计应沿用当前页面与风格：%+v", o)
	}
}

func TestSetupAdjustModeSelectChangesAction(t *testing.T) {
	r := newUIRig(t)
	r.putPage()
	r.activate()
	r.openTool(locales.ThemedReadingReaderAdjust())
	r.click(gfx.RoleCombobox, locales.ThemedReadingReaderAdjustMode())
	r.click(gfx.RoleOption, locales.ThemedReadingReaderAdjustRefresh())
	if !r.textHas(locales.ThemedReadingReaderResearchOption()) {
		t.Fatalf("按最新原文刷新应出现联网查证选项；画面上有 %q", r.session.Texts())
	}
	r.clickButton(locales.ThemedReadingGenerate())
	gens := r.svc.Calls().Generates
	if len(gens) != 1 || gens[0].Opts.Action != services.ThemedActionRefresh || gens[0].Opts.FromCurrent {
		t.Fatalf("刷新应重新读原文、不沿用当前页面：%+v", gens)
	}
}

func TestSetupRegenerateOpensCreateForm(t *testing.T) {
	r := newUIRig(t)
	r.putPage()
	r.activate()
	r.openTool(locales.ThemedReadingReaderRegenerate())
	r.clickButton(locales.ThemedReadingGenerate())
	gens := r.svc.Calls().Generates
	if len(gens) != 1 || gens[0].Opts.Action != services.ThemedActionCreate || gens[0].Opts.FromCurrent {
		t.Fatalf("重新生成应走全新生成：%+v", gens)
	}
}

func TestSetupModelsDisclosureShowsModelNames(t *testing.T) {
	r := newUIRig(t)
	r.svc.SetModels("gpt-text", "img-model")
	r.openSetup()
	if r.textHas("gpt-text") {
		t.Fatal("模型信息默认折叠")
	}
	r.clickButton(locales.ThemedReadingReaderModels())
	if !r.textHas("gpt-text") || !r.textHas("img-model") {
		t.Fatalf("展开后应显示模型名；画面上有 %q", r.session.Texts())
	}
}

func TestSetupReopenStartsFromFreshForm(t *testing.T) {
	r := newUIRig(t)
	r.openSetup()
	r.click(gfx.RoleCheckbox, locales.ThemedReadingGenerateImages())
	r.clickButton(locales.CommonCancel())
	r.clickButton(locales.ThemedReadingReaderCreate())
	r.clickButton(locales.ThemedReadingGenerate())
	gens := r.svc.Calls().Generates
	if len(gens) != 1 || !gens[0].Opts.GenerateImages {
		t.Fatalf("重新打开应回到默认表单（生图开启）：%+v", gens)
	}
}