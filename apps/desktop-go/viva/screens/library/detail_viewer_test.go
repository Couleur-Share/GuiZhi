package library

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/reader"
)

// 本文件测图片查看器的界面一侧（detailviewer.go）：阅读页点图片之后另开的整窗查看岛怎样出现、怎样收起。
// 查看岛宿主自己的握手、超时与校验在 viva/reader 里测过；这里同一个宿主配假打开器，查看页由测试扮演，不拉起 WebView2。

// viewerPage 是查看岛的页面（与阅读岛共用同一个假打开器，靠入口文件区分）；没开过就失败。
func (rr *readerRig) viewerPage() *fakeReaderPage {
	rr.t.Helper()
	for _, p := range rr.opener.pages {
		if p.cfg.Entry == "viewer.html" {
			return p
		}
	}
	rr.t.Fatal("没有打开过查看岛")
	return nil
}

func (rr *readerRig) viewerOpened() bool {
	for _, p := range rr.opener.pages {
		if p.cfg.Entry == "viewer.html" {
			return true
		}
	}
	return false
}

// lightbox 扮演阅读页点了图片：两张本地图，从第二张开始。
func (rr *readerRig) lightbox(p *fakeReaderPage) {
	rr.t.Helper()
	rr.event(p, map[string]any{"t": "lightbox", "index": 1, "images": []map[string]any{
		{"src": "local-image://a.png", "alt": "图 A"}, {"src": "local-image://b.png"},
	}})
	rr.frames(2)
}

// viewerReady 让查看页完成握手，再出几帧（岛被摆上、焦点交出去）。
func (rr *readerRig) viewerReady() *fakeReaderPage {
	rr.t.Helper()
	vp := rr.viewerPage()
	vp.send(rr.t, map[string]any{"t": "ready", "v": reader.ProtocolVersion})
	rr.frames(4)
	return vp
}

func TestLightboxEventOpensTheViewerIslandWithTheImagesAndTheCurrentLook(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t)
	rr.openCard(detailNoteID)
	p := rr.ready()
	if rr.host.Viewer().Active() || rr.viewerOpened() {
		t.Fatal("没有点图片之前不该有查看器")
	}
	rr.lightbox(p)
	if !rr.host.Viewer().Active() {
		t.Fatal("页面的 lightbox 事件应要求显示查看器")
	}
	vp := rr.viewerPage()
	if len(vp.places) != 0 {
		t.Fatal("握手之前查看岛不摆放：遮罩顶着")
	}
	vp = rr.viewerReady()
	init := vp.last(t, "init")
	if init["index"] != float64(1) {
		t.Errorf("起始序号应是页面给的第二张：%v", init["index"])
	}
	images, _ := init["images"].([]any)
	if len(images) != 2 || images[0].(map[string]any)["src"] != "local-image://a.png" || images[0].(map[string]any)["alt"] != "图 A" {
		t.Errorf("图片应原样交给查看页：%v", init["images"])
	}
	theme, _ := init["theme"].(map[string]any)
	if theme["basePx"] != float64(16) || init["lang"] != "zh" {
		t.Errorf("外观与语言取界面当前的值：%v %v", theme, init["lang"])
	}
	// 灯箱事件的回调里没有 ui.C，窗口宽度取最近一帧记下的值。
	if theme["viewportWidth"] != float64(1280) {
		t.Errorf("查看岛的外观要带窗口宽度：%v", theme["viewportWidth"])
	}
	if len(vp.places) == 0 {
		t.Fatal("握手之后应把查看岛摆上")
	}
	if got, want := vp.places[len(vp.places)-1], (gfx.Rect{Width: 1280, Height: 800}); got != want {
		t.Errorf("查看岛盖满整个窗口：%v，应为 %v", got, want)
	}
	if vp.focuses != 1 {
		t.Errorf("摆上之后应把焦点交给查看页一次：%d", vp.focuses)
	}
}

func TestClickingTheScrimBeforeTheIslandIsReadyClosesTheViewerAndSwallowsTheClick(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t)
	rr.openCard(detailNoteID)
	p := rr.ready()
	rr.lightbox(p)
	// 点在「专注」钮上：遮罩要把这一下吞掉，不能落到下面的控件。
	rr.click(gfx.RoleButton, locales.LibraryEnterFocusReading())
	if rr.host.Viewer().Active() {
		t.Fatal("点遮罩应收起查看器（渲染层 closeOnBackdropClick）")
	}
	if vp := rr.viewerPage(); vp.closes != 1 {
		t.Fatalf("收起时查看岛应销毁：%d", vp.closes)
	}
	if rr.screen.FocusReading() {
		t.Fatal("遮罩应吞掉这一下点击，下面的控件不该响应")
	}
	if p.focuses != 1 {
		t.Fatalf("收起之后键盘焦点交还阅读页：%d", p.focuses)
	}
}

func TestEscClosesTheViewerBeforeTheIslandIsReady(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t)
	rr.openCard(detailNoteID)
	p := rr.ready()
	rr.lightbox(p)
	rr.keys("escape")
	if rr.host.Viewer().Active() {
		t.Fatal("Esc 应收起查看器")
	}
	if rr.screen.FocusReading() {
		t.Fatal("Esc 只收起查看器")
	}
}

func TestViewerClosedByThePageReturnsTheKeyboardToTheReaderPage(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t)
	rr.openCard(detailNoteID)
	p := rr.ready()
	rr.lightbox(p)
	vp := rr.viewerReady()
	vp.send(rr.t, map[string]any{"t": "close"})
	rr.frames(3)
	if rr.host.Viewer().Active() || vp.closes != 1 {
		t.Fatalf("用户在页面里关闭之后查看岛应销毁：active=%v closes=%d", rr.host.Viewer().Active(), vp.closes)
	}
	if p.focuses != 1 {
		t.Fatalf("岛销毁时焦点落回窗口，要交还给阅读页：%d", p.focuses)
	}
	if rr.opener.opens != 2 {
		t.Fatalf("关闭之后不能重开：%d", rr.opener.opens)
	}
}

func TestViewerFailureIsReportedWithTheReason(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t)
	rr.openCard(detailNoteID)
	p := rr.ready()
	rr.opener.err = errors.New("没有可用的浏览器组件")
	rr.lightbox(p)
	text := strings.ReplaceAll(rr.toastText(), "\n", "")
	if !strings.Contains(text, locales.NativeIslandViewerAction()) || !strings.Contains(text, "没有可用的浏览器组件") {
		t.Fatalf("查看岛打不开要弹失败提示，带动作名与原因：\n%s", text)
	}
	if rr.host.Viewer().Active() {
		t.Fatal("打不开就不再显示")
	}
	if p.closes != 0 {
		t.Fatal("查看岛的失败不能连累阅读岛")
	}
}

func TestViewerLayerSitsAboveTheLiftedBars(t *testing.T) {
	t.Parallel()
	if kit.BandViewer <= kit.BandOverIslandInModal || kit.BandViewer <= kit.BandOverIsland {
		t.Errorf("查看器要盖住抬到岛之上的查看条与退出钮：%d 对 %d、%d", kit.BandViewer, kit.BandOverIsland, kit.BandOverIslandInModal)
	}
}

func TestViewerOverTheDetailModalOfTheTableView(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t)
	rr.openModal()
	p := rr.ready()
	rr.lightbox(p)
	if !rr.host.Viewer().Active() {
		t.Fatal("表格视图的详情浮层里也能打开查看器")
	}
	vp := rr.viewerReady()
	if got, want := vp.places[len(vp.places)-1], (gfx.Rect{Width: 1280, Height: 800}); got != want {
		t.Errorf("盖满整个窗口（含浮层）：%v，应为 %v", got, want)
	}
	rr.keys("escape")
	if rr.host.Viewer().Active() {
		t.Fatal("Esc 收起查看器")
	}
	if !rr.screen.detail.modal.open {
		t.Fatal("Esc 只收起查看器，不能连详情浮层一起关掉")
	}
	// 再开一次：这回点遮罩（浮层的遮罩在更低的一层，这一下不能落到它上面把浮层关掉）。
	rr.lightbox(p)
	if !rr.host.Viewer().Active() {
		t.Fatal("收起之后应能再开")
	}
	rr.clickAt(20, 20)
	if rr.host.Viewer().Active() {
		t.Fatal("点遮罩收起查看器")
	}
	if !rr.screen.detail.modal.open {
		t.Fatal("点查看器的遮罩不能落到浮层的遮罩上")
	}
}

// pixelAt 是整幅画面里 (x, y) 处的 RGB。
func (r *rig) pixelAt(x, y int) (red, green, blue int) {
	r.t.Helper()
	pix, w, _ := r.pixels()
	i := (y*w + x) * 4
	return int(pix[i]), int(pix[i+1]), int(pix[i+2])
}

// brightest 是 (x, y) 起 w×h 一块区域里最亮的一个通道值。
func (r *rig) brightest(x, y, w, h int) int {
	r.t.Helper()
	pix, width, _ := r.pixels()
	best := 0
	for row := y; row < y+h; row++ {
		for col := x; col < x+w; col++ {
			i := (row*width + col) * 4
			best = max(best, int(pix[i]), int(pix[i+1]), int(pix[i+2]))
		}
	}
	return best
}

// 遮罩是 ImageLightbox 容器的底色 rgba(0,0,0,.88)：盖满整个窗口，背后的画面只剩 12%。
func TestViewerScrimIsTheLightboxBackdropOverTheWholeWindow(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t)
	rr.openCard(detailNoteID)
	p := rr.ready()
	points := [][2]int{{20, 20}, {640, 400}, {1270, 790}, {300, 600}}
	var before [4][3]int
	for i, pt := range points {
		before[i][0], before[i][1], before[i][2] = rr.pixelAt(pt[0], pt[1])
	}
	rr.lightbox(p)
	for i, pt := range points {
		r, g, b := rr.pixelAt(pt[0], pt[1])
		got, base := [3]int{r, g, b}, before[i]
		for ch := 0; ch < 3; ch++ {
			if want := float64(base[ch]) * 0.12; float64(got[ch]) > want+2 || float64(got[ch]) < want-2 {
				t.Errorf("(%d,%d) 第 %d 个通道：遮罩之后 %d，应为原来 %d 的 12%%（约 %.1f）", pt[0], pt[1], ch, got[ch], base[ch], want)
			}
		}
	}
}

// 岛加载期间转圈延迟 250 ms 才出现（短促的加载不闪），岛握手之后就撤掉。
func TestViewerSpinnerWaitsBeforeAppearingAndGoesWhenTheIslandIsReady(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t)
	rr.openCard(detailNoteID)
	p := rr.ready()
	rr.lightbox(p)
	const dark = 40 // 遮罩之下的最亮通道值不会超过 255×0.12 的量级
	if got := rr.brightest(600, 360, 80, 80); got > dark {
		t.Fatalf("刚出现时只有遮罩：%d", got)
	}
	if err := rr.session.Advance(100 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	rr.frames(2)
	if got := rr.brightest(600, 360, 80, 80); got > dark {
		t.Fatalf("100 ms 时转圈还不该出现：%d", got)
	}
	if err := rr.session.Advance(400 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	rr.frames(3)
	if got := rr.brightest(600, 360, 80, 80); got <= dark {
		t.Fatalf("加载超过 250 ms 应出现转圈：%d", got)
	}
	rr.viewerReady()
	if got := rr.brightest(600, 360, 80, 80); got > dark {
		t.Fatalf("岛握手之后转圈要撤掉：%d", got)
	}
}

// scrimLayer 是整窗那块 rgba(0,0,0,.88) 的遮罩画在哪一层；没有就 ok 为假。
func (r *rig) scrimLayer() (layer uint16, ok bool) {
	r.t.Helper()
	for _, p := range r.session.Snapshot().Primitives {
		if p.Bounds == (gfx.Rect{Width: 1280, Height: 800}) && p.Color.Red == 0 && p.Color.Green == 0 && p.Color.Blue == 0 && p.Color.Alpha > 0.87 && p.Color.Alpha < 0.89 {
			return p.Layer, true
		}
	}
	return 0, false
}

// 遮罩画在查看器的层上（页面层里的东西、表格视图详情浮层的遮罩都在它之下）：两种详情位置都要对。
func TestViewerScrimIsPaintedOnTheViewerBand(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"卡片视图", "表格视图的详情浮层"} {
		rr := newReaderRig(t)
		if name == "卡片视图" {
			rr.openCard(detailNoteID)
		} else {
			rr.openModal()
		}
		p := rr.ready()
		if _, ok := rr.scrimLayer(); ok {
			t.Fatalf("%s：没有点图片之前不该有遮罩", name)
		}
		rr.lightbox(p)
		layer, ok := rr.scrimLayer()
		if !ok {
			t.Fatalf("%s：没找到整窗的遮罩", name)
		}
		if layer != uint16(kit.BandViewer) {
			t.Errorf("%s：遮罩在第 %d 层，应在查看器的第 %d 层", name, layer, kit.BandViewer)
		}
	}
}

// 每次打开转圈都重新延迟 250 ms：上一次已经转起来了，关掉再开，一开始不能直接转。
func TestViewerSpinnerDelayRestartsOnEveryOpening(t *testing.T) {
	t.Parallel()
	rr := newReaderRig(t)
	rr.openCard(detailNoteID)
	p := rr.ready()
	rr.lightbox(p)
	if err := rr.session.Advance(500 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	rr.frames(3)
	const dark = 40
	if got := rr.brightest(600, 360, 80, 80); got <= dark {
		t.Fatalf("前提：第一次打开 500 ms 后应在转：%d", got)
	}
	rr.clickAt(20, 20) // 收起
	rr.frames(2)
	rr.lightbox(p) // 再开
	if got := rr.brightest(600, 360, 80, 80); got > dark {
		t.Fatalf("再开的一瞬间转圈不该直接出现（延迟要重新计时）：%d", got)
	}
}

func TestViewerIsAbsentWithoutAHost(t *testing.T) {
	t.Parallel()
	r := detailRig(t)
	r.openCard(detailNoteID)
	r.frames(3)
	r.screen.showViewer([]reader.Image{{Src: "local-image://a.png"}}, 0) // 没接宿主：什么也不做，不 panic
	r.frames(2)
}
