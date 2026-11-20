package app

import (
	"image"
	"testing"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/screens/shell"
)

// ringPixels 数 rail（最左 railWidth 列）里颜色接近 (r, g, b) 的像素。
func ringPixels(img *image.RGBA, railWidth int, r, g, b int) int {
	n := 0
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < railWidth; x++ {
			p := img.RGBAAt(x, y)
			if abs8(p.R, r) <= 3 && abs8(p.G, g) <= 3 && abs8(p.B, b) <= 3 {
				n++
			}
		}
	}
	return n
}

func abs8(a uint8, b int) int {
	d := int(a) - b
	if d < 0 {
		return -d
	}
	return d
}

// openImportsRoot 打开采集模块，队列里有 running 个运行中、pending 个排队中的任务；返回时角标数已到位。
func openImportsRoot(t *testing.T, running, pending int) *rootFixture {
	t.Helper()
	svc := &servicestest.Imports{}
	svc.Set(services.ImportPage{Total: 1, Counts: map[string]int64{"all": 1, "completed": 1}, Entries: []services.ImportTask{importTask("t1", "示例页面")}}, nil)
	svc.SetQueueState(services.ImportQueueState{RunningCount: running, PendingCount: pending, Concurrency: 2})
	f := openRoot(t, func(o *Options) {
		o.Services.Imports = svc
		o.Page, o.Module = shell.PageHome, shell.ModuleImports
	})
	waitUntil(t, "首次读取队列", func() bool { return len(svc.Queries()) > 0 })
	waitUntil(t, "rail 角标取到进行中任务数", func() bool {
		f.frames(t, 1)
		return f.root.Window().ActiveImports == running+pending
	})
	f.frames(t, 2)
	return f
}

// 台账 G-019：rail 角标的描边是 Tailwind 缺省的环色 rgb(59 130 246 / 0.5)——类名 `ring-sidebar-background` 在渲染层不生成样式
// （tailwind.config.js 的 sidebar 色没有 background 项），`ring-2` 用缺省环色（渲染层事实：角标的 box-shadow 是
// rgba(59, 130, 246, 0.5) 0px 0px 0px 2px）。曾按类名画成 Sidebar 色，浅色主题下白底上的环看不出差别，
// 整窗逐像素对照也放过了（环只有约 126 个像素，远低于 L3 的 0.25% 比例上限，G-006）。
// 采集模块是当前模块，角标叠在高亮的导航项上（这个页面里是 (209, 216, 231)），环色叠上去是 (133, 173, 238)：
// 在 rail 里找这个颜色的像素，有角标时必须有一圈，没有角标时一个也没有。
func TestRootRailBadgeRingUsesTheDefaultRingColor(t *testing.T) {
	const railR, railG, railB = 133, 173, 238
	f := openImportsRoot(t, 1, 2)
	img, err := f.session.Image()
	if err != nil {
		t.Fatal(err)
	}
	railWidth := int(f.root.Look().Rem(5))
	// 角标高 1rem（16px）、环 2px，环带一圈约 120 个像素；盖在高亮导航项上的那一段才是这个颜色（角标伸出图标盒的 4px 外，环叠在 rail 底色上，颜色不同），实测 37 个。
	if ring := ringPixels(img, railWidth, railR, railG, railB); ring < 20 {
		t.Errorf("rail 里找到 %d 个环色像素，期望一圈（≥ 20，实测 37）：角标的描边应是 rgb(59 130 246 / 0.5) 叠在高亮导航项上", ring)
	}

	none := openImportsRoot(t, 0, 0)
	img, err = none.session.Image()
	if err != nil {
		t.Fatal(err)
	}
	if ring := ringPixels(img, railWidth, railR, railG, railB); ring != 0 {
		t.Errorf("没有进行中任务时没有角标，rail 里却有 %d 个环色像素", ring)
	}
}
