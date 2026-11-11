package main

// 窗口状态/启动可见性/托盘文案差分测试：golden.window 由真实 window-state.ts、tray-menu.ts 生成。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type windowGolden struct {
	Window struct {
		Resolve []struct {
			Persisted string `json:"persisted"`
			Displays  string `json:"displays"`
			Value     any    `json:"value"`
			Result    struct {
				Bounds         map[string]float64 `json:"bounds"`
				ShouldMaximize bool               `json:"shouldMaximize"`
			} `json:"result"`
		} `json:"resolve"`
		VisCases []struct {
			Input struct {
				Argv             []string `json:"argv"`
				OpenedAtLogin    bool     `json:"openedAtLogin"`
				MinimizeOnLaunch bool     `json:"minimizeOnLaunch"`
				ShouldMaximize   bool     `json:"shouldMaximize"`
			} `json:"input"`
			Result launchVisibility `json:"result"`
		} `json:"visCases"`
		ReadFile []struct {
			Name   string  `json:"name"`
			File   *string `json:"file"`
			Result struct {
				Bounds         map[string]float64 `json:"bounds"`
				ShouldMaximize bool               `json:"shouldMaximize"`
			} `json:"result"`
		} `json:"readFile"`
		CaptureCases []struct {
			Name    string                `json:"name"`
			Bounds  map[string]float64    `json:"bounds"`
			Max     bool                  `json:"max"`
			Full    bool                  `json:"full"`
			State   *persistedWindowState `json:"state"`
			Written *string               `json:"written"`
		} `json:"captureCases"`
		Labels []struct {
			Locale   string         `json:"locale"`
			Language string         `json:"language"`
			Labels   trayMenuLabels `json:"labels"`
		} `json:"labels"`
		Templates []struct {
			Visible bool   `json:"visible"`
			Lang    string `json:"lang"`
			Items   []struct {
				Label *string `json:"label"`
				Type  *string `json:"type"`
				Event *string `json:"event"`
			} `json:"items"`
		} `json:"templates"`
	} `json:"window"`
}

func displaySets() map[string][]windowBounds {
	d1 := windowBounds{0, 0, 1920, 1040}
	d2 := windowBounds{1920, 0, 1280, 984}
	dn := windowBounds{-1280, 100, 1280, 900}
	return map[string][]windowBounds{
		"one": {d1}, "two": {d1, d2}, "negative": {dn, d1}, "none": {},
		"small": {{0, 0, 900, 650}}, "tiny": {{0, 0, 640, 400}},
	}
}

// launchToMap 把 Go 结果转成与 TS 一致的形状：默认窗口只有宽高。
func launchToMap(s windowLaunchState) map[string]float64 {
	if !s.HasPosition {
		return map[string]float64{"width": float64(s.Bounds.Width), "height": float64(s.Bounds.Height)}
	}
	return map[string]float64{"x": float64(s.Bounds.X), "y": float64(s.Bounds.Y), "width": float64(s.Bounds.Width), "height": float64(s.Bounds.Height)}
}

func TestWindowStateParity(t *testing.T) {
	dir := os.Getenv("GUIZHI_GOLDEN_DIR")
	if dir == "" {
		t.Skip("未设置 GUIZHI_GOLDEN_DIR")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g windowGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	w := g.Window
	if len(w.Resolve) < 100 {
		t.Fatalf("golden 场景太少：%d", len(w.Resolve))
	}
	sets := displaySets()
	for _, c := range w.Resolve {
		got := resolveWindowLaunchState(c.Value, sets[c.Displays])
		if !reflect.DeepEqual(launchToMap(got), c.Result.Bounds) || got.ShouldMaximize != c.Result.ShouldMaximize {
			t.Errorf("%s × %s：got=%v max=%v want=%v max=%v", c.Persisted, c.Displays, launchToMap(got), got.ShouldMaximize, c.Result.Bounds, c.Result.ShouldMaximize)
		}
	}
	for i, c := range w.VisCases {
		in := c.Input
		if got := resolveLaunchVisibility(in.Argv, in.OpenedAtLogin, in.MinimizeOnLaunch, in.ShouldMaximize); got != c.Result {
			t.Errorf("vis[%d] %+v：got=%+v want=%+v", i, in, got, c.Result)
		}
	}
	for _, c := range w.ReadFile {
		f := filepath.Join(t.TempDir(), "s.json")
		if c.File != nil {
			os.WriteFile(f, []byte(*c.File), 0o644)
		}
		got := readWindowLaunchState(f, []windowBounds{{0, 0, 1920, 1040}})
		if !reflect.DeepEqual(launchToMap(got), c.Result.Bounds) || got.ShouldMaximize != c.Result.ShouldMaximize {
			t.Errorf("readFile %s：got=%v want=%v", c.Name, launchToMap(got), c.Result.Bounds)
		}
	}
	for _, c := range w.CaptureCases {
		b := [4]float64{c.Bounds["x"], c.Bounds["y"], c.Bounds["width"], c.Bounds["height"]}
		got := captureWindowState(windowBounds{}, c.Max, c.Full, &b)
		if !reflect.DeepEqual(got, c.State) {
			t.Errorf("capture %s：got=%+v want=%+v", c.Name, got, c.State)
			continue
		}
		if got != nil {
			f := filepath.Join(t.TempDir(), "w.json")
			if err := writeWindowState(f, *got); err != nil {
				t.Fatal(err)
			}
			b, _ := os.ReadFile(f)
			if c.Written == nil || string(b) != *c.Written {
				t.Errorf("write %s：got=%q want=%v", c.Name, b, c.Written)
			}
		}
	}
	for _, c := range w.Labels {
		if got := normalizeTrayMenuLanguage(c.Locale); got != c.Language {
			t.Errorf("language(%q)=%s want %s", c.Locale, got, c.Language)
		}
		if got := getTrayMenuLabels(c.Locale); got != c.Labels {
			t.Errorf("labels(%q)=%+v want %+v", c.Locale, got, c.Labels)
		}
	}
	for _, c := range w.Templates {
		got := buildTrayMenuItems(c.Visible, trayLabels[c.Lang])
		if len(got) != len(c.Items) {
			t.Fatalf("菜单项数 got=%d want=%d", len(got), len(c.Items))
		}
		for i, it := range c.Items {
			g := got[i]
			if g.Separator != (it.Type != nil && *it.Type == "separator") {
				t.Errorf("项 %d 分隔符不一致", i)
			}
			if !g.Separator && (it.Label == nil || *it.Label != g.Label) {
				t.Errorf("项 %d 标签 got=%q want=%v", i, g.Label, it.Label)
			}
			if !g.Separator && (it.Event == nil || *it.Event != g.Action) {
				t.Errorf("项 %d 动作 got=%q want=%v", i, g.Action, it.Event)
			}
		}
	}
}
