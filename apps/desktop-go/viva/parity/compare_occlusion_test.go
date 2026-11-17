package parity

import "testing"

func TestOverlayInkRectsCollectsOnlyVisibleOverlay(t *testing.T) {
	facts := &Facts{
		Texts: []Text{
			{Text: "页面文字", Rects: []Box{{10, 10, 40, 14}}},
			{Text: "气泡", Overlay: true, Rects: []Box{{100, 100, 50, 14}, {100, 114, 30, 14}}},
			{Text: "被裁掉的浮层文字", Overlay: true, Hidden: true, Rects: []Box{{300, 300, 20, 14}}},
		},
		OverlayIcons: []Box{{200, 200, 16, 16}},
	}
	got := overlayInkRects(facts)
	if len(got) != 3 {
		t.Fatalf("浮层墨迹应有 3 处（两行气泡文字 + 一枚图标），得到 %d：%v", len(got), got)
	}
	if overlayInkRects(nil) != nil {
		t.Fatal("没有事实时应返回空")
	}
}

func TestOccludedByOverlayInk(t *testing.T) {
	overlay := []Box{{100, 100, 50, 14}} // x 100..150，y 100..114
	cases := []struct {
		name string
		rect Box
		want bool
	}{
		{"完全重叠", Box{110, 102, 20, 10}, true},
		{"部分重叠", Box{140, 105, 30, 10}, true},
		{"离得远", Box{10, 10, 20, 10}, false},
		// 比较区域外扩 1 像素：外框右缘到 99 时区域延伸到 100，与浮层的左缘 100 只是贴边，不算
		{"贴边不算", Box{60, 102, 39, 10}, false},
		// 外框右缘 99.5：向外取整到 100 再外扩 1 像素到 101，侵入浮层 1 像素
		{"外扩一像素后侵入", Box{60, 102, 39.5, 10}, true},
		{"垂直方向离开", Box{110, 130, 20, 10}, false},
	}
	for _, c := range cases {
		if got := occludedByOverlayInk(c.rect, overlay); got != c.want {
			t.Errorf("%s：occludedByOverlayInk(%v) = %v，期望 %v", c.name, c.rect, got, c.want)
		}
	}
	if occludedByOverlayInk(Box{110, 102, 20, 10}, nil) {
		t.Error("没有浮层墨迹时不应判为被遮挡")
	}
}
