package settings_test

import (
	"testing"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 窄窗口里 AI 页是 grid 的两个 auto 行（台账 B-076）：侧栏与面板都超过一半高度时各分一半（H/2）；
// 侧栏没超过一半时保持自然高度，不再套一个 18rem（288px）的固定上限。

// mainTop 是右侧面板（窄窗口里的第二行）的上沿：面板标题在滚动区内边距（1.25rem）之下。
func mainTop(t *testing.T, r *rig) float32 {
	t.Helper()
	for _, tx := range r.s.Snapshot().Texts {
		if tx.Value == locales.SettingsAi() {
			return tx.Bounds.Y - 20
		}
	}
	t.Fatalf("没找到标题「%s」", locales.SettingsAi())
	return 0
}

func TestAINarrowRowsSplitHalfWhenBothExceedHalf(t *testing.T) {
	for _, tc := range []struct {
		name  string
		empty bool
	}{{"没有端点", true}, {"有端点", false}} {
		t.Run(tc.name, func(t *testing.T) {
			e := newAIEnv()
			if tc.empty {
				e.ai.PutConfig(services.AIConfig{Routes: map[string]string{}})
			}
			r, _ := e.openAISize(t, 800, 600)
			waitButton(r, locales.SettingsAddProvider())
			r.frames(6)
			if got := mainTop(t, r); got < 299 || got > 301 {
				t.Errorf("800×600 里侧栏应占一半高度（300px），面板上沿在 %v", got)
			}
		})
	}
}

func TestAINarrowAsideKeepsNaturalHeightBelowHalf(t *testing.T) {
	for _, tc := range []struct {
		name  string
		empty bool
	}{{"没有端点", true}, {"有端点", false}} {
		t.Run(tc.name, func(t *testing.T) {
			e := newAIEnv()
			if tc.empty {
				e.ai.PutConfig(services.AIConfig{Routes: map[string]string{}})
			}
			r, _ := e.openAISize(t, 800, 1000)
			waitButton(r, locales.SettingsAddProvider())
			r.frames(6)
			got := mainTop(t, r)
			if got <= 288 {
				t.Errorf("侧栏自然高度超过旧的 18rem（288px）上限，面板上沿应在其下：%v", got)
			}
			if got >= 500 {
				t.Errorf("侧栏没超过一半（500px）时不应被拉伸到一半：%v", got)
			}
		})
	}
}
