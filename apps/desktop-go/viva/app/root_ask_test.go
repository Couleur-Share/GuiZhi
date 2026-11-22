package app

import (
	"encoding/json"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/aiflow"
	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/locales"
	askscreen "guizhi-mygo-shell/viva/screens/ask"
	settingsscreen "guizhi-mygo-shell/viva/screens/settings"
	"guizhi-mygo-shell/viva/screens/shell"
)

// AI 问答接进 Root 的几处接线（root_ask.go）：模块挂的是真的 Screen、跨模块回调（配置模型、打开来源）、
// 采集结果的「就此提问」、以及 B-054（知识库彻底删除后清除问答里已展开的证据）。Screen 自己的行为在 viva/screens/ask 里测，这里只测接线。

// askTestSession 是一条带一个来源的会话：来源指向知识库条目 i1，证据正文是「证据正文-i1」。
func askTestSession(t *testing.T) services.AskSessionRecord {
	t.Helper()
	msg := services.AskMessage{
		ID: "m1", Question: "问题一", Answer: "结论见 [1]", Status: services.AskStatusDone,
		Sources: []services.AskSource{{
			Ordinal: 1, Kind: "item", RefID: "i1", Title: "来源条目",
			Evidence: &aiflow.EvidenceSnapshot{
				Version: 1, Kind: "item", Title: "来源条目", Text: "证据正文-i1", Fingerprint: "fp1",
				ReviewStatus: "clear", ReviewReasons: []string{},
			},
		}},
	}
	body, err := json.Marshal([]services.AskMessage{msg})
	if err != nil {
		t.Fatal(err)
	}
	return services.AskSessionRecord{ID: "s1", Title: "问题一", MessagesJSON: string(body)}
}

// askChipName 是来源芯片按钮的无障碍名。
const askChipName = "[1] 来源条目"

// hasChip 报告会话里的来源芯片是否已出现。
func (f *rootFixture) hasChip() bool {
	_, ok := f.session.AccessByRole(gfx.RoleButton, askChipName)
	return ok
}

// openAskRoot 打开 Root：问答与知识库都有数据（知识库里 i1 已在回收站），起始模块是问答。
func openAskRoot(t *testing.T, mutate func(*Options, *servicestest.AskFake)) (*rootFixture, *servicestest.AskFake, *servicestest.Library) {
	t.Helper()
	ask := &servicestest.AskFake{}
	ask.PutSession(askTestSession(t))
	lib := &servicestest.Library{}
	lib.Seed(services.LibraryCounts{All: 1, Trash: 1}, nil, nil)
	lib.SeedEntries([]services.LibraryEntry{{
		ID: "i1", Title: "来源条目", ItemType: services.LibraryTypeNote, Status: services.LibraryStatusActive,
		DeletedAt: 1_800_000_000_000, ReviewStatus: "clear", ReviewReasons: []string{},
		CreatedAt: 1_800_000_000_000, UpdatedAt: 1_800_000_000_000, Tags: []services.LibraryTag{},
	}})
	lib.SeedDetail("i1", servicestest.DetailBody{Content: "正文"})
	// 上次活跃会话记在偏好里：首帧恢复到 s1（没有它会落在空会话上）。
	prefs := &servicestest.Prefs{}
	_ = prefs.Set(askscreen.PrefActiveSession, "s1")
	f := openRoot(t, func(o *Options) {
		o.Page, o.Module = shell.PageHome, shell.ModuleAsk
		o.Services.Ask = ask
		o.Services.Library = lib
		o.Services.Prefs = prefs
		if mutate != nil {
			mutate(o, ask)
		}
	})
	return f, ask, lib
}

// 问答模块挂的是真的界面（不是「尚未迁移」占位）：进入后读到历史会话并打开它，消息与来源芯片出现。
func TestRootAskModuleMountsTheRealScreen(t *testing.T) {
	f, _, _ := openAskRoot(t, nil)
	f.waitFrames(t, "会话内容出现", func() bool { return f.session.HasText("问题一") && f.hasChip() })
	if f.session.HasText(locales.NativeShellNotMigrated(locales.NavAsk())) {
		t.Error("问答已接入，不应再显示「尚未迁移」占位")
	}
}

// 没有装配 Ask 的 Root（自动化实例）不挂问答模块：落在「尚未迁移」占位上，Close 不崩。
func TestRootWithoutAskService(t *testing.T) {
	f := openRoot(t, func(o *Options) { o.Page, o.Module = shell.PageHome, shell.ModuleAsk })
	if f.root.ask != nil || f.root.askStore != nil {
		t.Fatal("没有问答服务时不应装配问答模块")
	}
	f.root.Close()
}

// 模型没配好：空态上的「去配置模型」打开设置页的 AI 分区。
func TestRootAskConfigureModelOpensSettingsAI(t *testing.T) {
	f, _, _ := openAskRoot(t, func(_ *Options, ask *servicestest.AskFake) { ask.SetQAConfigured(false) })
	f.waitFrames(t, "会话内容出现", func() bool { return f.session.HasText("问题一") })
	f.clickButton(t, locales.AskNewSession())
	f.waitFrames(t, "配置模型入口出现", func() bool {
		_, ok := f.session.AccessByRole(gfx.RoleButton, locales.AskGoConfigure())
		return ok
	})
	f.clickButton(t, locales.AskGoConfigure())
	f.waitFrames(t, "打开设置页的 AI 分区", func() bool {
		return f.root.Window().Page == shell.PageSettings && f.root.settings.Active == settingsscreen.SectionAI
	})
}

// 点来源芯片上的「打开」类入口：条目来源在知识库里打开并选中（渲染层 onOpenSource）。
func TestRootAskOpenSourceGoesToLibraryAndSelectsIt(t *testing.T) {
	f, _, _ := openAskRoot(t, nil)
	f.waitFrames(t, "会话内容出现", f.hasChip)
	f.root.ask.Hooks.OpenSource(services.AskSource{Kind: "item", RefID: "i1"})
	store := f.root.library.Store
	f.waitFrames(t, "知识库里选中条目", func() bool {
		d := store.View().Detail
		return f.root.Window().Module == shell.ModuleLibrary && d.Item != nil && d.Item.ID == "i1"
	})
	if scope := store.View().Filters.Scope; scope != services.LibraryScopeAll {
		t.Errorf("范围应设为全部，实际 %q", scope)
	}
}

// Wiki 页来源指向 Wiki 模块（随 M3 的 Wiki 迁移落地，目前切过去是占位页）。
func TestRootAskOpenWikiSourceSwitchesToWikiModule(t *testing.T) {
	f, _, _ := openAskRoot(t, nil)
	f.waitFrames(t, "会话内容出现", f.hasChip)
	f.root.ask.Hooks.OpenSource(services.AskSource{Kind: "wiki", RefID: "p1"})
	f.waitFrames(t, "切到 Wiki 模块", func() bool { return f.root.Window().Module == shell.ModuleWiki })
}

// 采集结果的「就此提问」：切到问答、新建会话（不污染刚浏览的历史会话）、预填草稿但不发送。
func TestRootAskAboutItemJumpsWithDraft(t *testing.T) {
	imp := &servicestest.Imports{}
	imp.Set(services.ImportPage{}, nil)
	f, ask, _ := openAskRoot(t, func(o *Options, _ *servicestest.AskFake) {
		o.Services.Imports = imp
		o.Module = shell.ModuleImports
	})
	f.waitFrames(t, "进入采集模块", func() bool { return f.root.Window().Module == shell.ModuleImports })
	draft := locales.ImportsAskAboutItemDraft("某篇采集")
	f.root.imports.Hooks.AskAboutItem(draft)
	f.waitFrames(t, "切到问答并预填草稿", func() bool {
		return f.root.Window().Module == shell.ModuleAsk && f.session.HasText(draft)
	})
	view := f.root.askStore.View()
	if view.ActiveSessionID == "s1" || len(view.Messages) != 0 {
		t.Errorf("应新建空会话，不污染历史会话：active=%q 消息=%d", view.ActiveSessionID, len(view.Messages))
	}
	if n := len(ask.AskCalls()); n != 0 {
		t.Errorf("只预填不发送，实际发起了 %d 次提问", n)
	}
}

// B-054 端到端：已展开的证据在知识库彻底删除（选了「同时清除问答证据」）之后重新脱敏，正文不再显示、改为说明原因；
// 没选清除证据时证据原样保留。
func TestRootAskEvidenceClearedByLibraryHardDelete(t *testing.T) {
	f, _, lib := openAskRoot(t, nil)
	f.waitFrames(t, "会话内容出现", f.hasChip)
	f.clickButton(t, askChipName)
	f.waitFrames(t, "证据展开", func() bool { return f.session.HasText("证据正文-i1") })

	if !f.root.library.Store.DeleteForever(f.root.ctx, []string{"i1"}, true) {
		t.Fatal("彻底删除应成功")
	}
	if got := lib.ClearedEvidence(); len(got) != 1 || got[0] != "i1" {
		t.Fatalf("知识库侧应记录清除证据：%v", got)
	}
	f.waitFrames(t, "证据被清除并说明原因", func() bool { return f.session.HasText(locales.AskEvidenceClearedHint()) })
	if f.session.HasText("证据正文-i1") {
		t.Error("已清除的证据正文不能再显示")
	}
}

func TestRootAskEvidenceKeptWhenHardDeleteDoesNotClearIt(t *testing.T) {
	f, _, lib := openAskRoot(t, nil)
	f.waitFrames(t, "会话内容出现", f.hasChip)
	f.clickButton(t, askChipName)
	f.waitFrames(t, "证据展开", func() bool { return f.session.HasText("证据正文-i1") })

	if !f.root.library.Store.DeleteForever(f.root.ctx, []string{"i1"}, false) {
		t.Fatal("彻底删除应成功")
	}
	if got := lib.ClearedEvidence(); len(got) != 0 {
		t.Fatalf("没选清除证据就不应登记：%v", got)
	}
	f.frames(t, 4)
	if !f.session.HasText("证据正文-i1") || f.session.HasText(locales.AskEvidenceClearedHint()) {
		t.Error("没选清除证据时证据应原样保留")
	}
}
