package scenes_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"guizhi-mygo-shell/viva/parity"
	"guizhi-mygo-shell/viva/parity/scenes"
)

// TestScenesMatchReactFacts 把每个已登记场景在入库的 React 事实样本（viva/parity/testdata/react）上跑一遍 L1 + L2：
// 可访问树逐节点相等，外框差不超过 1px，文字行数、宽度与折行行距一致。
// 入库的只有事实 JSON：不需要 WebView2，也不依赖基准目录里的 PNG；像素与墨迹的比较见 cmd/vivaparity。
// 样本由 scripts/parity/export-testdata.mjs 导出；换了字体、令牌或屏幕后，样本要在重新采集后重新导出。
func TestScenesMatchReactFacts(t *testing.T) {
	th, err := parity.DefaultThresholds()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join("..", "testdata", "react")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		files, err := filepath.Glob(filepath.Join(root, e.Name(), "*.facts.json"))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			sceneID := strings.TrimSuffix(filepath.Base(file), ".facts.json")
			t.Run(e.Name()+"/"+sceneID, func(t *testing.T) {
				react, err := parity.LoadFacts(file)
				if err != nil {
					t.Fatal(err)
				}
				scene, err := scenes.Get(sceneID)
				if err != nil {
					t.Fatal(err)
				}
				viva, _, err := parity.Render(scene, react.Variant, react.Region)
				if err != nil {
					t.Fatal(err)
				}
				rep, _ := parity.Compare(parity.Inputs{React: react, Viva: viva, FactsOnly: true}, th)
				if !rep.Pass {
					for _, is := range rep.Issues {
						t.Errorf("[%s/%s] %s：%s", is.Layer, is.Kind, is.Subject, is.Message)
					}
				}
			})
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("没有入库的 React 事实样本可比较")
	}
}

// TestEveryRegisteredSceneHasSamples 守住「登记一个场景就要有样本」：没有样本的场景只在本机手动对照，CI 看不到。
func TestEveryRegisteredSceneHasSamples(t *testing.T) {
	root := filepath.Join("..", "testdata", "react")
	for _, id := range scenes.IDs() {
		files, _ := filepath.Glob(filepath.Join(root, "*", id+".facts.json"))
		if len(files) == 0 {
			t.Errorf("场景 %s 没有入库的 React 事实样本（node apps/desktop-go/scripts/parity/export-testdata.mjs）", id)
		}
	}
}
