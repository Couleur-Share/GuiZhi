//go:build windows

package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/viva/reader"
)

// ---- viewer：图片查看岛（第二座岛：viva/reader.Viewer 加渲染层自己的 ImageLightbox） ----

// viewerKeyScript 向查看页里拿着焦点的元素（没有就取灯箱容器）派发一个 keydown；yarl 的键盘导航收的是容器上冒泡来的事件。
func viewerKeyScript(key string) string {
	return fmt.Sprintf(`const a=document.activeElement;const t=a&&a!==document.body?a:(document.querySelector('.yarl__container')||document.body);`+
		`t.dispatchEvent(new KeyboardEvent('keydown',{key:%q,bubbles:true,cancelable:true}));return {dispatched:true,target:t.className||t.tagName}`, key)
}

// normCounter 去掉计数里的空白，"2 / 2" 与 "2/2" 视为相同。
func normCounter(v any) string {
	text, _ := v.(string)
	return strings.Join(strings.Fields(text), "")
}

func scViewer(s *smoke, r *result) {
	// 阅读岛用浅色：它的底色近白，查看页的底色是纯黑，哪座岛在上一眼分得清。
	view := reader.View{Kind: reader.ViewGallery, Content: galleryFixture}
	if _, ok := s.fresh(s.request(view, reader.TabImages, false)); !ok {
		r.check("阅读岛渲染", false, nil)
		return
	}
	settle()
	base, err := s.shot("viewer-0-reader")
	if err != nil {
		r.check("基线截图", false, err.Error())
		return
	}
	corner := s.at(base, 8, 8)
	r.check("基线：阅读岛的底色是浅色", luma(corner) > 0.8, rgb(corner))

	images := []reader.Image{{Src: "local-image://smoke-a.png", Alt: "图一"}, {Src: "local-image://smoke-b.png", Alt: "图二"}}
	open := func() (time.Duration, bool) {
		start := time.Now()
		s.onUI(func() { s.host.Viewer().Show(images, 1, s.theme16(false), reader.LangZh) })
		ok := s.waitUI(15*time.Second, func() bool { return s.host.Viewer().Ready() })
		return time.Since(start), ok
	}
	mark := s.mark()
	took, ok := open()
	r.set("openToReadyMs", ms(took))
	r.check("查看页握手完成（冷启动第二座岛）", ok, ms(took))
	if !ok {
		return
	}
	settle()
	var groupLen int
	s.onUI(func() { groupLen = s.group.Len() })
	r.check("此刻有两座岛（阅读岛与查看岛）", groupLen == 2, groupLen)
	r.check("岛摆上之后焦点交给了查看页", len(s.eventsSince(mark, "focus")) > 0 && s.eventsSince(mark, "focus")[0].Data == "viewer.html", s.eventsSince(mark, "focus"))

	dom, ok := s.probe("viewer-dom", `const imgs=[...document.images];const c=document.querySelector('.yarl__counter');`+
		`return {n:imgs.length,loaded:imgs.filter(i=>i.complete&&i.naturalWidth>0).length,srcs:imgs.map(i=>i.getAttribute('src')),`+
		`counter:c?c.textContent:null,root:!!document.querySelector('.yarl__root'),toolbar:!!document.querySelector('.yarl__toolbar'),`+
		`lang:document.documentElement.lang,bg:getComputedStyle(document.body).backgroundColor}`, 5*time.Second)
	if !ok {
		r.check("探针回报", false, nil)
		return
	}
	r.set("dom", dom)
	r.check("页面里是 yarl 的灯箱（根与工具条）", dom["root"] == true && dom["toolbar"] == true, dom)
	loaded, _ := dom["loaded"].(float64)
	r.check("当前这张图真的加载了（经 gzviewer 的 media 路径）", loaded >= 1 && strings.Contains(fmt.Sprint(dom["srcs"]), "smoke-b.png"), dom["srcs"])
	r.check("计数是 2 / 2（起始序号 1）", normCounter(dom["counter"]) == "2/2", dom["counter"])

	img1, err := s.shot("viewer-1-open")
	if err != nil {
		r.check("截图", false, err.Error())
		return
	}
	for _, pt := range [][2]float64{{8, 8}, {950, 630}, {480, 20}} {
		c := s.at(img1, pt[0], pt[1])
		r.check(fmt.Sprintf("(%.0f,%.0f) 是查看页的黑底：查看岛盖住了阅读岛", pt[0], pt[1]), luma(c) < 0.03, rgb(c))
	}
	mid := s.at(img1, 480, 320)
	r.check("窗口中央是第二张图（橙色）", int(mid.R)-int(mid.B) > 30, rgb(mid))

	x0, y0, _, _, _ := clientOnScreen(s.hwnd)
	for _, pt := range [][2]float64{{8, 8}, {480, 320}, {950, 630}} {
		leaf, owned := windowAt(s.hwnd, x0+int(pt[0]*s.scale), y0+int(pt[1]*s.scale))
		r.check(fmt.Sprintf("(%.0f,%.0f) 的命中归岛（不是窗口自己）", pt[0], pt[1]), owned && leaf != vivaWindowClass && leaf != "", map[string]any{"类名": leaf, "属于本窗口": owned})
	}

	// 合成的键盘：方向键换图（计数跟着变）。真实键盘与指针不在这里测（台账 R-15）。
	if res, ok := s.probe("viewer-left", viewerKeyScript("ArrowLeft"), 5*time.Second); !ok || res["dispatched"] != true {
		r.check("派发 ArrowLeft", false, res)
	} else {
		settle()
		c, _ := s.probe("viewer-c1", `const c=document.querySelector('.yarl__counter');return {counter:c?c.textContent:null}`, 5*time.Second)
		r.check("ArrowLeft 换到第一张：计数 1 / 2", normCounter(c["counter"]) == "1/2", c)
	}
	if res, ok := s.probe("viewer-right", viewerKeyScript("ArrowRight"), 5*time.Second); !ok || res["dispatched"] != true {
		r.check("派发 ArrowRight", false, res)
	} else {
		settle()
		c, _ := s.probe("viewer-c2", `const c=document.querySelector('.yarl__counter');return {counter:c?c.textContent:null}`, 5*time.Second)
		r.check("ArrowRight 回到第二张：计数 2 / 2", normCounter(c["counter"]) == "2/2", c)
	}

	// 更高一层的不透明原生控件（提示、确认的层）盖得住查看岛。
	magenta := gfx.Color{Red: 1, Green: 0, Blue: 1, Alpha: 1}
	rect := gfx.Rect{X: 560, Y: 40, Width: 300, Height: 50}
	s.onUI(func() { s.float = &floatSpec{band: 6, rect: rect, col: magenta} })
	settle()
	if img, err := s.shot("viewer-2-toast-band"); err != nil {
		r.check("提示层截图", false, err.Error())
	} else {
		p := s.at(img, float64(rect.X+rect.Width/2), float64(rect.Y+rect.Height/2))
		r.check("第 6 层（提示）的色块画在查看岛之上", near(p, colorOf(magenta), 6), rgb(p))
	}
	s.onUI(func() { s.float = nil })

	// Esc 关闭：页面发 close，宿主销毁岛，阅读岛原样还在。
	closedMark := s.mark()
	if res, ok := s.probe("viewer-esc", viewerKeyScript("Escape"), 5*time.Second); !ok || res["dispatched"] != true {
		r.check("派发 Escape", false, res)
		return
	}
	_, closed := s.waitEvent(closedMark, "viewer:closed", 5*time.Second, nil)
	r.check("Esc 之后页面发出 close，宿主回调 Closed", closed, nil)
	gone := s.waitUI(3*time.Second, func() bool { return !s.host.Viewer().Active() && !s.host.Viewer().Open() })
	r.check("查看器结束显示、岛已销毁", gone, nil)
	settle()
	s.onUI(func() { groupLen = s.group.Len() })
	r.check("销毁之后 Group 里只剩阅读岛", groupLen == 1, groupLen)
	img3, err := s.shot("viewer-3-closed")
	if err != nil {
		r.check("关闭后截图", false, err.Error())
		return
	}
	for _, pt := range [][2]float64{{8, 8}, {950, 630}} {
		a, b := s.at(img3, pt[0], pt[1]), s.at(base, pt[0], pt[1])
		r.check(fmt.Sprintf("(%.0f,%.0f) 阅读岛原样露出", pt[0], pt[1]), near(a, b, 8), map[string]string{"此刻": rgb(a), "基线": rgb(b)})
	}

	// 再开一次（新岛），这回从界面一侧收起（点遮罩或 Esc 落到 Hide）：岛销毁，不回调 Closed。
	mark2 := s.mark()
	took2, ok := open()
	r.set("reopenToReadyMs", ms(took2))
	r.check("收起之后能再开", ok, ms(took2))
	if ok {
		settle()
		s.onUI(func() { s.host.Viewer().Hide() })
		gone = s.waitUI(3*time.Second, func() bool { return !s.host.Viewer().Open() })
		r.check("界面一侧收起：岛销毁", gone, nil)
		r.check("界面一侧收起不回调 Closed", len(s.eventsSince(mark2, "viewer:closed")) == 0, nil)
		s.onUI(func() { groupLen = s.group.Len() })
		r.check("再次销毁之后 Group 里只剩阅读岛", groupLen == 1, groupLen)
	}
	r.check("全程没有失败回调", len(s.eventsSince(mark, "viewer:failed")) == 0, s.eventsSince(mark, "viewer:failed"))
}
