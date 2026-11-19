package app

import (
	"math"
	"testing"

	"guizhi-mygo-shell/native/settings"
)

// Root 把悬停刷新（kit.Look.HoverRefresh）与知识库列表的窗口级键盘（Look.KeyHook）接到每一个新装配的 Look 上；
// 外观变了重装配 Look 之后也要接上（Look 是按外观重建的）。请求经 Options.HoverRefreshHere 出去，测试里是记录器。
func TestRootWiresHoverRefreshAndKeyHookIntoEveryLook(t *testing.T) {
	f := openRoot(t, nil)
	look := f.root.Look()
	if look.HoverRefresh == nil || look.KeyHook == nil {
		t.Fatalf("Look 应已接上 HoverRefresh 与 KeyHook：%v %v", look.HoverRefresh != nil, look.KeyHook != nil)
	}
	before := f.hoverHere
	look.RequestHoverRefresh()
	if f.hoverHere != before+1 {
		t.Fatalf("请求一次悬停刷新应经 Options.HoverRefreshHere 出去一次，实际 %d → %d", before, f.hoverHere)
	}

	// 外观变了：Look 被重建，新的 Look 仍要接上。
	if _, err := f.fake.Update(func(v *settings.Values) { v.UI.ThemeHue = math.Mod(v.UI.ThemeHue+40, 360) }); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 4)
	next := f.root.Look()
	if next == look {
		t.Fatal("夹具失效：改了主题色相之后 Look 应被重建")
	}
	if next.HoverRefresh == nil || next.KeyHook == nil {
		t.Fatalf("重建的 Look 也要接上 HoverRefresh 与 KeyHook：%v %v", next.HoverRefresh != nil, next.KeyHook != nil)
	}
}
