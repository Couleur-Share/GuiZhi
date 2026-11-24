package settings_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	nsettings "guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/internal/frametime"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/parity"
	sscreen "guizhi-mygo-shell/viva/screens/settings"
	shellscreen "guizhi-mygo-shell/viva/screens/shell"
)

// 壁纸与 AI 品牌图标的帧耗时（M3-6c Step 3b，模块完成定义第 4 条：稳态帧 ≤ 2 ms、滚动一步 ≤ 6 ms）。
// 口径与同包 TestFrameBudget 一致：Frame() 只记录不栅格化，量界面线程的出帧成本；栅格化加回读（Image()）另量一次作上界。
// 默认跳过：VIVA_FRAME_BUDGET=1 时运行。

// budgetWallpaper 是一个壁纸子场景的背景图设置。
type budgetWallpaper struct {
	enabled bool
	opacity float64
	blur    float64
}

// wallpaperBudgetRig 是整窗（顶栏 + rail + 设置页「显示设置」）壁纸场景的无头会话，与 viva/parity/scenes/settings_wallpaper.go 同一套装配：
// 同一张合成壁纸夹具、shellscreen.Window.Wallpaper 画整窗壁纸、预览舞台由设置页自己的缓存画。
type wallpaperBudgetRig struct {
	*rig
	shell *sscreen.Shell
	cache *kit.WallpaperCache
}

// openWallpaperBudget 打开整窗壁纸场景，滚到内容底，等到预览缓存（以及启用时的整窗缓存）渲染完成后返回。
func openWallpaperBudget(t *testing.T, ws budgetWallpaper) *wallpaperBudgetRig {
	t.Helper()
	fontOnce.Do(func() { app.SetInterfaceFontFamily("Microsoft YaHei UI") })
	spec := kit.DefaultSpec()
	spec.Motion = kit.MotionLevelOff
	spec.Glass = ws.enabled
	look := kit.NewLook(spec)
	look.Scale = 1
	look.Viewport = 1280
	load := func(fileName string) ([]byte, error) {
		if fileName != parity.WallpaperFixtureName {
			return nil, fmt.Errorf("wallpaper fixture: unknown file %q", fileName)
		}
		return parity.WallpaperFixture(), nil
	}
	v := nsettings.Default()
	v.UI.BackgroundImageFileName = parity.WallpaperFixtureName
	v.UI.BackgroundImageEnabled = ws.enabled
	v.UI.BackgroundImageOpacity = ws.opacity
	v.UI.BackgroundImageBlur = ws.blur
	shell := &sscreen.Shell{Look: look, Store: sscreen.NewMemStore(v), Active: sscreen.SectionAppearance, LoadBackground: load, Wake: func() {}}
	t.Cleanup(shell.Release)
	window := &shellscreen.Window{Look: look, Page: shellscreen.PageSettings, OwnControls: true, SearchShortcut: "Alt+Shift+F", Settings: shell}
	cache := kit.NewWallpaperCache(load, func() {})
	t.Cleanup(cache.Close)
	window.Wallpaper = func(d *ui.DrawContext) {
		if !look.Glass {
			cache.Bitmap("", 0, 0, 0, 0) // 与 Root.paintWallpaper 相同：壁纸没有启用时清缓存、什么也不画
			return
		}
		look.PaintWallpaperLayer(d, kit.WallpaperLayer{
			Cache: cache, File: parity.WallpaperFixtureName,
			Opacity: nsettings.ClampOpacity(ws.opacity), Blur: nsettings.ClampBlur(ws.blur), Tokens: look.T,
		})
	}
	bg := look.Col(look.T.Background)
	page := ui.NewPage(ui.PageConfig{
		View: func(c ui.C) {
			c.Canvas().SetReduceMotion(true)
			look.BeginPaint()
			window.View(c)
		},
		Theme: &look.Theme, Background: &bg,
	})
	var canvas gfx.Canvas
	session, err := headless.Open(page.Config(app.Config{Title: t.Name(), Width: 1280, Height: 800}), &canvas, headless.Options{Scale: 1})
	if err != nil {
		t.Fatalf("打开无头会话失败：%v", err)
	}
	t.Cleanup(session.Close)
	r := &wallpaperBudgetRig{rig: &rig{t: t, s: session}, shell: shell, cache: cache}
	r.frames(4)
	r.scrollToEnd() // 预览舞台在内容底部：进入视口才会读图
	srcW, srcH := parity.WallpaperFixtureSize()
	srcBytes := srcW * srcH * 4 // 解码后的源位图字节数：缓存里多出渲染结果（Retained 超过它）才算位图渲染完成
	waitUntil(r.rig, "预览舞台壁纸渲染完成", func() bool { return shell.PreviewRetained() > srcBytes })
	if ws.enabled {
		waitUntil(r.rig, "整窗壁纸渲染完成", func() bool { return cache.Retained() > srcBytes })
	}
	r.frames(5)
	return r
}

// retainedKiB 是预览舞台缓存与整窗缓存此刻占用的位图 KiB。
func (r *wallpaperBudgetRig) retainedKiB() (preview, window int) {
	return r.shell.PreviewRetained() / 1024, r.cache.Retained() / 1024
}

// budgetAIConfig 是「多供应商、多端点、多模型」的 AI 配置：10 个供应商（覆盖有品牌 logo 的分类与首字母圆），每个 12 个模型，
// 供应商行（17px）、端点卡头部与模型行（20px）都画真实栅格化的品牌图标，端点卡够长、能滚动。
func budgetAIConfig() services.AIConfig {
	cfg := services.AIConfig{Routes: map[string]string{}}
	kinds := []string{"openai", "anthropic", "google", "deepseek", "qwen", "moonshot", "mistral", "xai", "zhipu", "ernie"}
	for i, kind := range kinds {
		id := fmt.Sprintf("p%d", i)
		url := fmt.Sprintf("https://api.prov%d.example.com", i)
		cfg.Providers = append(cfg.Providers, services.AIProvider{
			ID: id, Name: fmt.Sprintf("Prov%d", i), Provider: kind, APIProtocol: services.AIProtocolOpenAI, APIKey: aiSecret, APIURL: url, Enabled: true,
		})
		for j := 0; j < 12; j++ {
			m := services.AIModel{
				ID: fmt.Sprintf("m%d-%d", i, j), ProviderID: id, Provider: kind, APIProtocol: services.AIProtocolOpenAI, APIKey: aiSecret, APIURL: url,
				Model: fmt.Sprintf("%s-model-%02d", kind, j), Enabled: true, Capabilities: services.AICapabilities{Chat: true},
			}
			if j == 0 {
				m.IsDefault = i == 0
				m.Capabilities.Vision = true
			}
			if j == 1 {
				m.Capabilities = services.AICapabilities{Embedding: true}
			}
			cfg.Models = append(cfg.Models, m)
		}
	}
	return cfg
}

// TestWallpaperAndBrandIconFrameBudget 读出 M3-6c 新增的重度子场景的帧耗时：壁纸预览舞台与整窗玻璃模式三态（选图未启用 / 启用 / 启用并虚化），
// 以及 AI 品牌图标（工作台多供应商 + 供应商类型下拉打开）。结果记进开发文档 §8.4。
func TestWallpaperAndBrandIconFrameBudget(t *testing.T) {
	if os.Getenv("VIVA_FRAME_BUDGET") == "" {
		t.Skip("帧耗时读数与机器有关，默认不跑：VIVA_FRAME_BUDGET=1 时运行")
	}
	type opened struct {
		r *rig
		// 记录缓存占用（KiB）的回调；AI 场景没有壁纸缓存，返回 -1。
		retained func() (preview, window int)
	}
	wallpaper := func(ws budgetWallpaper) func(t *testing.T) opened {
		return func(t *testing.T) opened {
			r := openWallpaperBudget(t, ws)
			return opened{r: r.rig, retained: r.retainedKiB}
		}
	}
	noCache := func() (int, int) { return -1, -1 }
	pages := []struct {
		name string
		open func(t *testing.T) opened
		// atEnd 为真表示页面打开后已滚到内容底：滚动读数先向上滚再回来。
		atEnd    bool
		noScroll bool
	}{
		{"appearance_wallpaper_disabled", wallpaper(budgetWallpaper{enabled: false, opacity: 0.7, blur: 0}), true, false},
		{"appearance_wallpaper_enabled", wallpaper(budgetWallpaper{enabled: true, opacity: 0.35, blur: 0}), true, false},
		{"appearance_wallpaper_blurred", wallpaper(budgetWallpaper{enabled: true, opacity: 0.55, blur: 14}), true, false},
		{"ai_workbench_brand_icons", func(t *testing.T) opened {
			e := newAIEnv()
			e.ai.PutConfig(budgetAIConfig())
			r, canvas := e.openAICanvas(t)
			waitUntil(r, "供应商行出现", func() bool { return squareImages(canvas, 17) >= 9 })
			// 选第一个供应商：端点卡头部与 12 个模型行各一枚 20px 品牌图标。
			var label string
			for _, n := range r.s.Access() {
				if n.Role == gfx.RoleButton && strings.Contains(n.Label, " Prov0 ") {
					label = n.Label
					break
				}
			}
			if label == "" {
				t.Fatal("没有找到第一个供应商行")
			}
			r.clickNode(gfx.RoleButton, label)
			waitUntil(r, "端点卡出现", func() bool {
				return r.has(gfx.RoleButton, locales.SettingsTestConnection()) && squareImages(canvas, 20) >= 13
			})
			return opened{r: r, retained: noCache}
		}, false, false},
		{"ai_provider_select_open", func(t *testing.T) opened {
			e := newAIEnv()
			r, canvas := e.openAICanvas(t)
			waitButton(r, mainProviderBtn)
			r.clickNode(gfx.RoleButton, locales.SettingsAddProvider())
			waitUntil(r, "添加弹窗", func() bool { return r.dialogOpen(locales.SettingsAiWorkbenchAddProvider()) })
			r.clickNode(gfx.RoleCombobox, locales.SettingsAiWorkbenchProviderType())
			waitUntil(r, "选项展开", func() bool { return len(r.named(gfx.RoleOption, "OpenAI")) > 0 && squareImages(canvas, 18) >= 10 })
			return opened{r: r, retained: noCache}
		}, false, true},
	}
	ms := func(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
	for _, pg := range pages {
		t.Run(pg.name, func(t *testing.T) {
			heapBefore := frametime.HeapInUse()
			o := pg.open(t)
			r := o.r
			heapKiB := (int64(frametime.HeapInUse()) - int64(heapBefore)) / 1024
			previewKiB, windowKiB := o.retained()
			t.Logf("%s 打开页面后 Go 堆增量（含无头会话）：%d KiB；预览缓存 Retained %d KiB，整窗缓存 Retained %d KiB（-1 表示没有壁纸缓存）", pg.name, heapKiB, previewKiB, windowKiB)

			idle, err := frametime.Measure(300, func(int) error { return r.s.Frame() })
			if err != nil {
				t.Fatalf("稳态帧失败：%v", err)
			}
			t.Logf("%s 稳态帧（布局+记录）：%s", pg.name, idle)

			points := [][2]float32{{640, 300}, {640, 380}}
			hover, err := frametime.Measure(200, func(i int) error {
				p := points[i%2]
				if err := r.s.Move(p[0], p[1]); err != nil {
					return err
				}
				return r.s.Frame()
			})
			if err != nil {
				t.Fatalf("悬停帧失败：%v", err)
			}
			t.Logf("%s 悬停帧（移动指针+出帧）：%s", pg.name, hover)

			var scroll frametime.Stats
			if !pg.noScroll {
				// 已在内容底的页面先向上滚（再回来），否则先向下滚（再回来）。
				dir := float32(1)
				if pg.atEnd {
					dir = -1
				}
				scrollStep := func(dy float32) error {
					if err := r.s.Scroll(gfx.ScrollEvent{X: 640, Y: 450, DeltaY: dy, Precise: true}); err != nil {
						return err
					}
					return r.s.Frame()
				}
				start := frametime.Fingerprint(r.s.Access())
				for i := 0; i < 20; i++ {
					if err := scrollStep(40 * dir); err != nil {
						t.Fatalf("预检滚动失败：%v", err)
					}
				}
				if start == frametime.Fingerprint(r.s.Access()) {
					t.Fatalf("滚动 800 px 之后画面没有变化，滚动读数无效（页面不够长，或滚动位置不在滚动区里）")
				}
				for i := 0; i < 20; i++ {
					if err := scrollStep(-40 * dir); err != nil {
						t.Fatalf("预检回滚失败：%v", err)
					}
				}
				scroll, err = frametime.Measure(200, func(i int) error {
					if (i/20)%2 == 1 {
						return scrollStep(-40 * dir)
					}
					return scrollStep(40 * dir)
				})
				if err != nil {
					t.Fatalf("滚动帧失败：%v", err)
				}
				t.Logf("%s 滚动一步（滚动+出帧）：%s", pg.name, scroll)
			}

			raster, err := frametime.Measure(30, func(int) error {
				_, err := r.s.Image()
				return err
			})
			if err != nil {
				t.Fatalf("栅格化失败：%v", err)
			}
			t.Logf("%s 栅格化+回读（Image，上界）：%s", pg.name, raster)
			t.Logf("摘要 %s | 稳态 mean=%.2f p95=%.2f | 悬停 mean=%.2f p95=%.2f | 滚轮 mean=%.2f p95=%.2f | 栅格化 mean=%.2f | 堆增量 %d KiB | 预览 Retained %d KiB | 整窗 Retained %d KiB",
				pg.name, ms(idle.Mean), ms(idle.P95), ms(hover.Mean), ms(hover.P95), ms(scroll.Mean), ms(scroll.P95), ms(raster.Mean), heapKiB, previewKiB, windowKiB)
		})
	}
}
