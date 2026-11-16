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
				t.Parallel() // 每次渲染都有自己的外观、页面与无头会话，互不共享状态；并行把总耗时压在 10 秒预算内
				react, err := parity.LoadFacts(file)
				if err != nil {
					t.Fatal(err)
				}
				scene, err := scenes.Get(sceneID)
				if err != nil {
					t.Fatal(err)
				}
				viva, err := parity.RenderFacts(scene, react.Variant, react.Region)
				if err != nil {
					t.Fatal(err)
				}
				rep, _ := parity.Compare(parity.Inputs{React: react, Viva: viva, FactsOnly: true, Waivers: scene.Waivers}, th)
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

// TestWaiversPointToLedger 守住「豁免必须有台账条目」：每个 Waiver 的编号要在台账里作为条目标题（### A-006 …）
// 或表格行（| B-006 | …）真实存在，没登记的偏差不能靠 Waiver 溜过去。
func TestWaiversPointToLedger(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "docs", "agents", "viva-parity-ledger.md"))
	if err != nil {
		t.Fatal(err)
	}
	ledger := strings.ReplaceAll(string(data), "\r\n", "\n")
	waivers := 0
	for _, id := range scenes.IDs() {
		scene, err := scenes.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range scene.Waivers {
			waivers++
			if !strings.Contains(ledger, "### "+w.Ledger+" ") && !strings.Contains(ledger, "| "+w.Ledger+" |") {
				t.Errorf("场景 %s 的 Waiver（%s/%s %s）指向的台账条目 %s 不存在", id, w.Layer, w.Kind, w.Subject, w.Ledger)
			}
			if strings.TrimSpace(w.Note) == "" {
				t.Errorf("场景 %s 的 Waiver（%s %s）没有说明", id, w.Kind, w.Subject)
			}
		}
	}
	if waivers == 0 {
		t.Skip("没有任何场景登记 Waiver")
	}
}
