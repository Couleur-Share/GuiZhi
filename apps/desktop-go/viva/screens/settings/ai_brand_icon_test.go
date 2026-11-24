package settings_test

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	sscreen "guizhi-mygo-shell/viva/screens/settings"
)

// AI 供应商品牌图标（台账 B-075）：各处的图标尺寸与渲染层一一对应——
// 供应商行徽章 17px、端点卡片头部与模型行 20px、拉取模型列表 16px、供应商类型下拉的触发器与每个选项 18px。

// openAICanvas 同 aiEnv.open，另外留下画布，用来读当前帧里画出的位图。
func (e *aiEnv) openAICanvas(t *testing.T) (*rig, *gfx.Canvas) {
	t.Helper()
	return e.openAISize(t, 1280, 800)
}

// openAISize 同 openAICanvas，窗口大小由调用方给。
func (e *aiEnv) openAISize(t *testing.T, width, height int) (*rig, *gfx.Canvas) {
	t.Helper()
	fontOnce.Do(func() { app.SetInterfaceFontFamily("Microsoft YaHei UI") })
	spec := kit.DefaultSpec()
	spec.Motion = kit.MotionLevelOff
	look := kit.NewLook(spec)
	look.Scale = 1
	look.Viewport = float32(width)
	a := &sscreen.AI{Look: look, Guard: e.guard(), Service: e.ai, Wake: func() {}}
	t.Cleanup(a.Close)
	bg := look.Col(look.T.Background)
	page := ui.NewPage(ui.PageConfig{
		View: func(c ui.C) {
			c.Canvas().SetReduceMotion(true)
			a.Page(c)
		},
		Theme: &look.Theme, Background: &bg,
	})
	var canvas gfx.Canvas
	session, err := headless.Open(page.Config(app.Config{Title: t.Name(), Width: width, Height: height}), &canvas, headless.Options{Scale: 1})
	if err != nil {
		t.Fatalf("打开无头会话失败：%v", err)
	}
	t.Cleanup(session.Close)
	r := &rig{t: t, s: session}
	r.frames(4)
	return r, &canvas
}

// squareImages 是当前帧里边长恰为 size 的方形位图个数（品牌图标是位图或渐变盒，位图 logo 才计入）。
func squareImages(canvas *gfx.Canvas, size float32) int {
	var n int
	for _, img := range canvas.Images() {
		if img.Bounds.Width == size && img.Bounds.Height == size {
			n++
		}
	}
	return n
}

func TestAIProviderRowAndEndpointCardRenderBrandIcons(t *testing.T) {
	e := newAIEnv()
	r, canvas := e.openAICanvas(t)
	waitButton(r, mainProviderBtn)
	if n := squareImages(canvas, 17); n != 1 {
		t.Errorf("供应商行应有 1 个 17px 品牌图标，得到 %d", n)
	}
	if n := squareImages(canvas, 20); n != 0 {
		t.Errorf("没选端点时不应有 20px 图标，得到 %d", n)
	}

	r.clickNode(gfx.RoleButton, mainProviderBtn)
	waitUntil(r, "端点卡", func() bool { return r.hasText("gpt-4o") && r.has(gfx.RoleButton, locales.SettingsTestConnection()) })
	// 端点卡头部 1 个 + 两个模型行各 1 个，都是 20px；供应商行仍是 17px。
	if n := squareImages(canvas, 20); n != 3 {
		t.Errorf("端点卡应有 3 个 20px 品牌图标（头部 + 2 个模型行），得到 %d", n)
	}
	if n := squareImages(canvas, 17); n != 1 {
		t.Errorf("供应商行仍应是 1 个 17px 图标，得到 %d", n)
	}
}

func TestAIFetchModelGroupHeadersRenderBrandIcons(t *testing.T) {
	e := newAIEnv()
	e.ai.PutFetchResult(services.AIFetchModelsResult{Success: true, Models: []services.AIRemoteModel{
		{ID: "gpt-4.1", OwnedBy: "openai"}, {ID: "gpt-4o-mini", OwnedBy: "openai"}, {ID: "claude-3-5-sonnet", OwnedBy: "anthropic"}, {ID: "gemini-1.5-pro", OwnedBy: "google"},
	}})
	r, canvas := e.openAICanvas(t)
	waitButton(r, mainProviderBtn)
	r.clickNode(gfx.RoleButton, mainProviderBtn)
	waitUntil(r, "端点卡", func() bool { return r.has(gfx.RoleButton, locales.SettingsFetchModels()) })
	if n := squareImages(canvas, 16); n != 0 {
		t.Fatalf("拉取之前不应有 16px 图标：%d", n)
	}
	r.clickNode(gfx.RoleButton, locales.SettingsFetchModels())
	waitUntil(r, "模型列表", func() bool { return r.has(gfx.RoleButton, locales.SettingsSelectModel("gpt-4.1")) })
	if n := squareImages(canvas, 16); n != 3 {
		t.Errorf("拉取列表每个家族分组头部一个 16px 品牌图标（GPT / Claude / Gemini 三组）：期望 3，得到 %d", n)
	}
}

func TestAIProviderTypeSelectRendersBrandIconsInTriggerAndOptions(t *testing.T) {
	e := newAIEnv()
	r, canvas := e.openAICanvas(t)
	waitButton(r, mainProviderBtn)
	r.clickNode(gfx.RoleButton, locales.SettingsAddProvider())
	waitUntil(r, "添加弹窗", func() bool { return r.dialogOpen(locales.SettingsAiWorkbenchAddProvider()) })
	if n := squareImages(canvas, 18); n != 1 {
		t.Fatalf("选了预设时下拉触发器应有 1 个 18px 图标，得到 %d", n)
	}
	r.clickNode(gfx.RoleCombobox, locales.SettingsAiWorkbenchProviderType())
	waitUntil(r, "选项展开", func() bool { return len(r.named(gfx.RoleOption, "OpenAI")) > 0 })
	// 触发器 1 个 + 每个有品牌 logo 的预设一个选项图标，全是 18px。
	// Custom / Other 是渐变盒、不是位图，不计入；选项列表是滚动区，没滚到的行也画（被裁掉），所以按预设总数算。
	want := 1
	for _, p := range services.AIProviderPresets() {
		if p.IconCategory != "Custom" && p.IconCategory != "Other" {
			want++
		}
	}
	if n := squareImages(canvas, 18); n != want {
		t.Errorf("18px 位图图标应为触发器 1 个加每个有 logo 的预设一个：期望 %d，得到 %d", want, n)
	}
}
