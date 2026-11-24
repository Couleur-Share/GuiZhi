package settings_test

import (
	"testing"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 窄窗口里 AI 页是 grid 的两个 auto 行（台账 B-076）：侧栏与面板都超过一半高度时，在各自的基础高度之上平分剩下的高度
// （面板滚动区上下内边距 40px 缩不掉，侧栏有 border-b 时再加 1px；渲染层 800×600 实测 256 / 296 与 256.5 / 295.5）；
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
			// 可用高度 600：基础高度之上平分，侧栏 =（600 − 40）/ 2，有供应商时多 border-b 的 0.5（（600 − 41）/ 2 + 1）；
			// 面板上沿按标题文字框上推 20px 量，允许 ±1.5 的取整与文字框偏差。
			want := float32(280)
			if !tc.empty {
				want = 280.5
			}
			if got := mainTop(t, r); got < want-1.5 || got > want+1.5 {
				t.Errorf("800×600 里侧栏应在基础高度之上平分（约 %vpx），面板上沿在 %v", want, got)
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
