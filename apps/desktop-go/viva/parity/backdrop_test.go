package parity

import (
	"strings"
	"testing"
)

// TestBackdropPanelNameFnIsReadWhenResolving 守住 PanelNameFn 的「比较时才取」：场景在 init 里登记，那时界面语言还是默认的中文，
// 英文变体要等 -lang en 设定之后才取得到英文的面板名。这里在登记之后才改名字：找不到面板时的报错里应是改后的名字，
// 而不是登记时的 PanelName（也不是登记那一刻 PanelNameFn 的返回值）。
func TestBackdropPanelNameFnIsReadWhenResolving(t *testing.T) {
	name := "登记时的名称"
	spec := &BackdropSpec{PanelRole: "group", PanelName: "静态名称", PanelNameFn: func() string { return name }}
	name = "比较时的名称"
	_, err := spec.Resolve(Scene{}, &Facts{}, false)
	if err == nil {
		t.Fatal("Viva 事实里没有面板，Resolve 应报错")
	}
	if !strings.Contains(err.Error(), "「比较时的名称」") {
		t.Errorf("报错里应是比较时才取到的面板名，实际：%v", err)
	}
	if strings.Contains(err.Error(), "静态名称") || strings.Contains(err.Error(), "登记时的名称") {
		t.Errorf("报错里不应出现静态名称或登记时的名称：%v", err)
	}
}

// TestBackdropPanelNameFallsBackToThePlainName 守住没有 PanelNameFn 时仍按 PanelName 找面板（旧场景的写法不受影响）。
func TestBackdropPanelNameFallsBackToThePlainName(t *testing.T) {
	spec := &BackdropSpec{PanelRole: "group", PanelName: "静态名称"}
	_, err := spec.Resolve(Scene{}, &Facts{}, false)
	if err == nil || !strings.Contains(err.Error(), "group「静态名称」") {
		t.Errorf("没有 PanelNameFn 时报错里应是 PanelName，实际：%v", err)
	}
}
