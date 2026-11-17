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
		if got := occludedByOverlayInk(c.rect, overlay, nil); got != c.want {
			t.Errorf("%s：occludedByOverlayInk(%v) = %v，期望 %v", c.name, c.rect, got, c.want)
		}
	}
	if occludedByOverlayInk(Box{110, 102, 20, 10}, nil, nil) {
		t.Error("没有浮层墨迹时不应判为被遮挡")
	}
}

// 浮层面板（取色器弹层、下拉面板、对话框、气泡）压在页面文字上：文字没有墨迹可比，与浮层墨迹同一规则。
func TestOverlayPanelRectsAndOcclusion(t *testing.T) {
	facts := &Facts{Nodes: []Node{
		{Role: "button", Name: "自定义", Box: []float64{10, 10, 40, 36}},
		{Role: "dialog", Name: "自定义", Box: []float64{854, 454, 232, 230}},
		{Role: "listbox", Name: "语言", Box: []float64{100, 100, 180, 118}},
		{Role: "dialog", Name: "未渲染", Box: nil},
	}}
	panels := overlayPanelRects(facts)
	if len(panels) != 2 || panels[0] != (Box{854, 454, 232, 230}) || panels[1] != (Box{100, 100, 180, 118}) {
		t.Fatalf("应只取有外框的面板角色（对话框、下拉面板）：%v", panels)
	}
	if overlayPanelRects(nil) != nil {
		t.Fatal("没有事实时应返回空")
	}
	if !occludedByOverlayInk(Box{1055, 415.75, 24.5, 16}, nil, []Box{{877, 401, 232, 218.5}}) {
		t.Error("整个盖在面板底下的文字应判为被遮挡")
	}
	if !occludedByOverlayInk(Box{1100, 500, 30, 16}, nil, []Box{{877, 401, 232, 218.5}}) {
		t.Error("只盖住一部分（压在面板边缘）的文字应判为被遮挡")
	}
	if occludedByOverlayInk(Box{700, 415.75, 24.5, 16}, nil, []Box{{877, 401, 232, 218.5}}) {
		t.Error("在面板之外的文字不应判为被遮挡")
	}
}
