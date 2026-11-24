package settings_test

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"

	nsettings "guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	sscreen "guizhi-mygo-shell/viva/screens/settings"
)

// 背景图预览舞台（台账 B-020）：选了图之后舞台是 16:9 的缩微外壳叠在壁纸上；纯绘制，不进可访问树。

// previewPNG 是 64×36 的纯色 PNG（16:9）。
func previewPNG(t *testing.T, c color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 64, 36))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// bgLoader 是可注入结果、可卡住的读图函数，并记录读取的文件名。
type bgLoader struct {
	mu    sync.Mutex
	data  []byte
	err   error
	gate  chan struct{} // 非 nil 时先等它关闭（模拟读取中）
	names []string
}

func (l *bgLoader) load(name string) ([]byte, error) {
	l.mu.Lock()
	l.names = append(l.names, name)
	data, err, gate := l.data, l.err, l.gate
	l.mu.Unlock()
	if gate != nil {
		<-gate
	}
	return data, err
}

func (l *bgLoader) reads() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.names...)
}

// withImage 是已选 wall.png 的设置；opacity 与 blur 由调用方给。
func withImage(enabled bool, opacity, blur float64) *sscreen.MemStore {
	v := nsettings.Default()
	v.UI.BackgroundImageFileName, v.UI.BackgroundImageEnabled = "wall.png", enabled
	v.UI.BackgroundImageOpacity, v.UI.BackgroundImageBlur = opacity, blur
	return sscreen.NewMemStore(v)
}

// openPreview 打开显示设置页并滚到背景图分区。
func openPreview(t *testing.T, store sscreen.Store, tweak func(a *sscreen.Appearance)) (*rig, *sscreen.Appearance) {
	t.Helper()
	r, a := openAppearanceWith(t, store, func(a *sscreen.Appearance) {
		a.Wake = func() {}
		if tweak != nil {
			tweak(a)
		}
	})
	r.scrollToEnd()
	return r, a
}

// reddish 是壁纸透出来的像素：壁纸是纯红，叠上半透明的浅色遮罩与缩微面板后仍是 R=255、G=B 的粉红；\r\n// 界面其余部分是中性色，文字的亚像素边缘也不会正好 G=B。
func reddish(c color.RGBA) bool {
	return c.R == 255 && c.G == c.B && c.G > 20 && c.G < 220
}

// stageBounds 是截图里偏红像素的外接矩形与个数。
func (r *rig) stageBounds() (b image.Rectangle, n int) {
	r.t.Helper()
	img, err := r.s.Image()
	if err != nil {
		r.t.Fatal(err)
	}
	b = image.Rectangle{Min: image.Pt(1<<20, 1<<20), Max: image.Pt(-1, -1)} // 不能用 image.Rect：它会把反向的角规整掉
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			if !reddish(img.RGBAAt(x, y)) {
				continue
			}
			n++
			b.Min.X, b.Min.Y = min(b.Min.X, x), min(b.Min.Y, y)
			b.Max.X, b.Max.Y = max(b.Max.X, x+1), max(b.Max.Y, y+1)
		}
	}
	if n == 0 {
		return image.Rectangle{}, 0
	}
	return b, n
}

// stageLoaded 等到预览读完图并画出来。
func (r *rig) stageLoaded(a *sscreen.Appearance) {
	r.t.Helper()
	waitUntil(r, "预览位图读完", func() bool { return a.PreviewRetained() > 0 })
	r.frames(3)
}

// 有图时舞台画出缩微外壳与壁纸：偏红区域是 16:9 的矩形，里面有缩微界面的多层色块；整个舞台不增加可访问节点。
func TestBackgroundPreviewStageDrawsMiniatureShell(t *testing.T) {
	load := &bgLoader{data: previewPNG(t, color.NRGBA{R: 255, A: 255})}
	r, a := openPreview(t, withImage(true, 1, 0), func(a *sscreen.Appearance) { a.LoadBackground = load.load })
	r.stageLoaded(a)
	if got := load.reads(); len(got) != 1 || got[0] != "wall.png" {
		t.Fatalf("应按设置里的文件名读一次图：%q", got)
	}
	b, n := r.stageBounds()
	if n == 0 {
		t.Fatal("预览里没有壁纸透出来")
	}
	if w, h := float64(b.Dx()), float64(b.Dy()); w/h < 1.5 || w/h > 1.9 {
		t.Errorf("舞台应是 16:9 左右：%v×%v", w, h)
	}
	if w := b.Dx(); w < 240 {
		t.Errorf("舞台过小：宽 %d", w)
	}
	img, err := r.s.Image()
	if err != nil {
		t.Fatal(err)
	}
	distinct := map[color.RGBA]bool{}
	for y := b.Min.Y; y < b.Max.Y; y += 2 {
		for x := b.Min.X; x < b.Max.X; x += 2 {
			distinct[img.RGBAAt(x, y)] = true
		}
	}
	if len(distinct) < 4 {
		t.Errorf("舞台里应有壁纸与缩微外壳的多层色块，只看到 %d 种颜色", len(distinct))
	}
	// 纯绘制：舞台范围内没有任何可访问节点。
	for _, node := range r.s.Access() {
		c := node.Bounds
		cx, cy := int(c.X+c.Width/2), int(c.Y+c.Height/2)
		if image.Pt(cx, cy).In(b) {
			t.Errorf("舞台里不应有可访问节点：%v「%s」", node.Role, node.Label)
		}
	}
	// 与没有图的页面相比，控件的可访问节点集合不变。
	empty, _ := openPreview(t, sscreen.NewMemStore(nsettings.Default()), nil)
	if got, want := len(r.s.Access()), len(empty.s.Access()); got != want {
		t.Errorf("有图时可访问节点数 %d，没有图时 %d：预览舞台不应增加节点", got, want)
	}
}

// 舞台固定用壁纸（玻璃）令牌：「开启」与「关闭」两种状态下舞台里的像素完全一样（预览的是开启后的样子）。
func TestBackgroundPreviewStageIgnoresEnabledFlag(t *testing.T) {
	data := previewPNG(t, color.NRGBA{R: 255, A: 255})
	stage := func(enabled bool) (*image.RGBA, image.Rectangle) {
		r, a := openPreview(t, withImage(enabled, 1, 0), func(a *sscreen.Appearance) {
			a.LoadBackground = func(string) ([]byte, error) { return data, nil }
		})
		r.stageLoaded(a)
		b, _ := r.stageBounds()
		img, err := r.s.Image()
		if err != nil {
			t.Fatal(err)
		}
		return img, b
	}
	on, onBounds := stage(true)
	off, offBounds := stage(false)
	if onBounds != offBounds || onBounds.Empty() {
		t.Fatalf("两种状态下舞台位置应一致：%v %v", onBounds, offBounds)
	}
	for y := onBounds.Min.Y; y < onBounds.Max.Y; y++ {
		for x := onBounds.Min.X; x < onBounds.Max.X; x++ {
			if on.RGBAAt(x, y) != off.RGBAAt(x, y) {
				t.Fatalf("(%d,%d) 开启 %v 与关闭 %v 不同", x, y, on.RGBAAt(x, y), off.RGBAAt(x, y))
			}
		}
	}
}

// 可见度与虚化：可见度越低壁纸越淡；有虚化时另渲染一份位图（占用多于源图），虚化为 0 时只占源图。
func TestBackgroundPreviewFollowsOpacityAndBlur(t *testing.T) {
	data := previewPNG(t, color.NRGBA{R: 255, A: 255})
	run := func(opacity, blur float64) (*rig, *sscreen.Appearance, int) {
		r, a := openPreview(t, withImage(true, opacity, blur), func(a *sscreen.Appearance) {
			a.LoadBackground = func(string) ([]byte, error) { return data, nil }
		})
		r.stageLoaded(a)
		_, n := r.stageBounds()
		return r, a, n
	}
	_, aSharp, full := run(1, 0)
	_, _, faint := run(0.1, 0)
	if faint >= full {
		t.Errorf("可见度 10%% 的偏红像素（%d）应少于 100%%（%d）", faint, full)
	}
	src := 64 * 36 * 4
	if got := aSharp.PreviewRetained(); got != src {
		t.Errorf("不虚化且可见度 100%% 时只占源图：%d，期望 %d", got, src)
	}
	_, aBlur, _ := run(1, 12)
	waitFor := func() bool { return aBlur.PreviewRetained() > src }
	deadline := time.Now().Add(2 * time.Second)
	for !waitFor() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !waitFor() {
		t.Errorf("有虚化时应另渲染一份位图：%d", aBlur.PreviewRetained())
	}
}

// 选图成功：自动启用并出现预览；点「关闭」后预览保留（只翻转启用）。
func TestBackgroundPickSuccessShowsPreviewThenDisableKeepsIt(t *testing.T) {
	data := previewPNG(t, color.NRGBA{R: 255, A: 255})
	load := &bgLoader{data: data}
	store := sscreen.NewMemStore(nsettings.Default())
	r, a := openPreview(t, store, func(a *sscreen.Appearance) {
		a.PickBackground = func() (string, error) { return "wall.png", nil }
		a.LoadBackground = load.load
	})
	if _, n := r.stageBounds(); n != 0 || a.PreviewRetained() != 0 || len(load.reads()) != 0 {
		t.Fatal("没选图时不应有预览位图，也不应读图")
	}
	r.clickNode(gfx.RoleButton, locales.SettingsSelectBackgroundImage())
	waitUntil(r, "选图结果写入", func() bool { return store.Values().UI.BackgroundImageFileName == "wall.png" })
	r.stageLoaded(a)
	if v := store.Values().UI; !v.BackgroundImageEnabled {
		t.Fatalf("选图后应自动启用：%+v", v)
	}
	if _, n := r.stageBounds(); n == 0 {
		t.Fatal("选图后舞台里应出现壁纸")
	}
	if !r.has(gfx.RoleButton, locales.SettingsChangeBackgroundImage()) {
		t.Error("选图后按钮应换成「更换图片」")
	}

	r.clickLast(gfx.RoleButton, locales.SettingsDisableBackgroundImage())
	if store.Values().UI.BackgroundImageEnabled {
		t.Fatal("点「关闭」应翻转启用")
	}
	if _, n := r.stageBounds(); n == 0 || a.PreviewRetained() == 0 {
		t.Errorf("关闭只翻转启用，预览应保留：偏红像素 %d、占用 %d", n, a.PreviewRetained())
	}
	r.clickLast(gfx.RoleButton, locales.SettingsEnableBackgroundImage())
	if !store.Values().UI.BackgroundImageEnabled {
		t.Error("再点「开启」应重新启用")
	}
}

// 取消选图：不读图、没有预览、设置不变。
func TestBackgroundPickCancelLeavesNoPreview(t *testing.T) {
	load := &bgLoader{data: previewPNG(t, color.NRGBA{R: 255, A: 255})}
	store := sscreen.NewMemStore(nsettings.Default())
	picked := make(chan struct{}, 1)
	r, a := openPreview(t, store, func(a *sscreen.Appearance) {
		a.PickBackground = func() (string, error) { picked <- struct{}{}; return "", nil }
		a.LoadBackground = load.load
	})
	r.clickNode(gfx.RoleButton, locales.SettingsSelectBackgroundImage())
	select {
	case <-picked:
	case <-time.After(2 * time.Second):
		t.Fatal("选图函数没有被调用")
	}
	time.Sleep(20 * time.Millisecond)
	r.frames(3)
	if _, n := r.stageBounds(); n != 0 || a.PreviewRetained() != 0 || len(load.reads()) != 0 || store.Updates != 0 {
		t.Errorf("取消后不应有预览或读图或写入：偏红 %d、占用 %d、读取 %q、写入 %d", n, a.PreviewRetained(), load.reads(), store.Updates)
	}
}

// 选图成功但保存设置失败、选图本身失败：都弹错误提示，原文放进可复制的详情；设置不变、没有预览。
func TestBackgroundPickFailuresToastWithDetail(t *testing.T) {
	run := func(t *testing.T, pick func() (string, error), failSave error) (*sysEnv, *sscreen.MemStore, *sscreen.Appearance) {
		env := newEnv()
		store := sscreen.NewMemStore(nsettings.Default())
		store.FailNext = failSave
		r, a := openPreview(t, store, func(a *sscreen.Appearance) {
			a.Guard = env.guard()
			a.PickBackground = pick
			a.LoadBackground = func(string) ([]byte, error) { return nil, errors.New("not reached") }
		})
		r.clickNode(gfx.RoleButton, locales.SettingsSelectBackgroundImage())
		waitUntil(r, "弹出错误提示", func() bool { return len(env.toasts(kit.ToastError)) > 0 })
		r.frames(3)
		return env, store, a
	}
	t.Run("保存失败", func(t *testing.T) {
		env, store, a := run(t, func() (string, error) { return "wall.png", nil }, errors.New("disk full: settings.json"))
		toasts := env.toasts(kit.ToastError)
		if len(toasts) != 1 || !strings.Contains(toasts[0].Detail, "disk full") {
			t.Fatalf("应有一条带原因详情的错误提示：%+v", toasts)
		}
		if v := store.Values().UI; v.BackgroundImageFileName != "" {
			t.Errorf("保存失败时设置应不变：%+v", v)
		}
		if a.PreviewRetained() != 0 {
			t.Error("保存失败时不应有预览")
		}
	})
	t.Run("选图失败", func(t *testing.T) {
		env, store, _ := run(t, func() (string, error) { return "", errors.New("copy failed: access denied") }, nil)
		toasts := env.toasts(kit.ToastError)
		if len(toasts) != 1 || toasts[0].Message != locales.SettingsBackgroundImageSaveFailed() || !strings.Contains(toasts[0].Detail, "access denied") {
			t.Fatalf("选图失败应弹固定提示并带原因详情：%+v", toasts)
		}
		if store.Updates != 0 {
			t.Errorf("选图失败不应写入设置：%d", store.Updates)
		}
	})
}

// 读图失败：舞台画损坏的回退（与「读取中什么都不画」不同），不占位图内存，也没有壁纸透出来。
func TestBackgroundPreviewReadFailureDrawsFallback(t *testing.T) {
	gate := make(chan struct{})
	t.Cleanup(func() { close(gate) })
	loading := &bgLoader{gate: gate}
	rLoading, _ := openPreview(t, withImage(true, 1, 0), func(a *sscreen.Appearance) { a.LoadBackground = loading.load })
	rLoading.frames(3)
	imgLoading, err := rLoading.s.Image()
	if err != nil {
		t.Fatal(err)
	}

	failing := &bgLoader{err: errors.New("open wall.png: file not found")}
	r, a := openPreview(t, withImage(true, 1, 0), func(a *sscreen.Appearance) { a.LoadBackground = failing.load })
	waitUntil(r, "读图失败", func() bool { return len(failing.reads()) > 0 })
	time.Sleep(20 * time.Millisecond)
	r.frames(4)
	if a.PreviewRetained() != 0 {
		t.Errorf("读图失败不应占位图内存：%d", a.PreviewRetained())
	}
	if _, n := r.stageBounds(); n != 0 {
		t.Errorf("读图失败时不应有壁纸透出：%d", n)
	}
	img, err := r.s.Image()
	if err != nil {
		t.Fatal(err)
	}
	var diff int
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			if img.RGBAAt(x, y) != imgLoading.RGBAAt(x, y) {
				diff++
			}
		}
	}
	if diff < 200 {
		t.Errorf("读取失败应比读取中多画出回退底与图标，只差 %d 个像素", diff)
	}
	if got := failing.reads(); len(got) != 1 {
		t.Errorf("失败后不应反复重试读取：%q", got)
	}

	// 宿主没接读图：同样按失败画回退，不 panic。
	r2, a2 := openPreview(t, withImage(true, 1, 0), nil)
	r2.frames(4)
	if a2.PreviewRetained() != 0 {
		t.Error("没接读图时不应有位图")
	}
}

// 没有图时舞台不建缓存；选了图又换回没有图（清空文件名）时释放缓存。
func TestBackgroundPreviewReleasesWhenImageCleared(t *testing.T) {
	data := previewPNG(t, color.NRGBA{R: 255, A: 255})
	store := withImage(true, 1, 0)
	r, a := openPreview(t, store, func(a *sscreen.Appearance) {
		a.LoadBackground = func(string) ([]byte, error) { return data, nil }
	})
	r.stageLoaded(a)
	if err := store.Update(func(v *nsettings.Values) { v.UI.BackgroundImageFileName = "" }); err != nil {
		t.Fatal(err)
	}
	r.frames(3)
	if a.PreviewRetained() != 0 {
		t.Errorf("清空文件名后应释放预览：%d", a.PreviewRetained())
	}
	// Close 可重复调用，之后再有图会重新读取。
	a.Close()
	a.Close()
	if err := store.Update(func(v *nsettings.Values) { v.UI.BackgroundImageFileName = "wall.png" }); err != nil {
		t.Fatal(err)
	}
	r.stageLoaded(a)
}
