package scenes_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"guizhi-mygo-shell/viva/parity"
	"guizhi-mygo-shell/viva/parity/scenes"
)

// redPin 固定一个「已登记在台账、对照目前仍未通过」的场景此刻的失败形态（M2-4 起，台账 G-029）。
type redPin struct {
	Ledger string   // 对应的未关闭台账条目
	Kinds  []string // 允许出现的问题类别（层/类别），多出别的类别就是有新的偏差混进来了
	Max    int      // 事实比较（L1+L2）里问题条数的上限，来自登记当时的实测
}

// knownRed 里的场景在 CI 里**不算通过**：它们的红色照旧写在台账里、照旧在 vivaparity 的报告里（判定不变），这里只是把
// 失败形态固定下来，让 CI 既不被已登记的偏差挡住，也看得见混在里面的新偏差——出现表外的类别或条数超上限都会失败；
// 场景已经通过也会失败，提醒把它从这张表去掉并关闭台账条目（见 TestKnownRedPointsToOpenLedgerItems）。
// 这不是 Waiver：不改任何阈值、比较器或放过条款，也不让场景在 vivaparity 里变绿。
var knownRed = map[string]redPin{
	// B-062：标题框单行，不折行也不随内容增高，头部以下整页差 31 px
	"app.window.library.detail-long-title": {Ledger: "B-062", Kinds: []string{"L2/box", "L2/icon-extra", "L2/icon-missing", "L2/text-lines"}, Max: 21},
	// G-027：L1 角色映射里没有 region 与 ListMarker（映射表有哈希守卫，扩展待追认）
	"app.window.library.detail-review":      {Ledger: "G-027", Kinds: []string{"L1/extra-node", "L1/unmapped-role"}, Max: 2},
	"app.window.library.detail-review-open": {Ledger: "G-027", Kinds: []string{"L1/extra-node", "L1/unmapped-role"}, Max: 3},
	// G-027（M2-6b 延伸）：工具侧栏是带名字的 region，信息栏是 dl/dt/dd（DescriptionList、term、definition），评论卡片是 article 与 time——
	// 都没有映射规则；Viva 把侧栏表达成 group，于是多一个 extra-node。这些场景的 L2、L3 全部通过，只因 L1 未通过。
	"app.window.library.detail-tools-note":             {Ledger: "G-027", Kinds: []string{"L1/extra-node", "L1/unmapped-role"}, Max: 5},
	"app.window.library.detail-tools-note-picker":      {Ledger: "G-027", Kinds: []string{"L1/extra-node", "L1/unmapped-role"}, Max: 5},
	"app.window.library.detail-tools-note-handoff":     {Ledger: "G-027", Kinds: []string{"L1/extra-node", "L1/unmapped-role"}, Max: 5},
	"app.window.library.detail-tools-webpage":          {Ledger: "G-027", Kinds: []string{"L1/extra-node", "L1/unmapped-role"}, Max: 5},
	"app.window.library.detail-tools-webpage-open":     {Ledger: "G-027", Kinds: []string{"L1/extra-node", "L1/unmapped-role"}, Max: 5},
	"app.window.library.detail-tools-webpage-selected": {Ledger: "G-027", Kinds: []string{"L1/extra-node", "L1/unmapped-role"}, Max: 5},
	"app.window.library.detail-tools-comments":         {Ledger: "G-027", Kinds: []string{"L1/extra-node", "L1/unmapped-role"}, Max: 5},
	"app.window.library.detail-tools-comments-open":    {Ledger: "G-027", Kinds: []string{"L1/extra-node", "L1/unmapped-role"}, Max: 7},
	"app.window.library.detail-tools-comments-empty":   {Ledger: "G-027", Kinds: []string{"L1/extra-node", "L1/unmapped-role"}, Max: 5},
	// G-027（M2-6c 延伸）：AI 摘要展开后的 Markdown 正文多出 strong、ListMarker、code 三个未映射角色，其余同上；
	// 其余场景（摘要折叠、OCR 两态、标签建议）与评论一样只有 region / DescriptionList / term / definition 与 extra-node。
	"app.window.library.detail-tools-summary-collapsed": {Ledger: "G-027", Kinds: []string{"L1/extra-node", "L1/unmapped-role"}, Max: 5},
	"app.window.library.detail-tools-summary-expanded":  {Ledger: "G-027", Kinds: []string{"L1/extra-node", "L1/unmapped-role"}, Max: 8},
	"app.window.library.detail-tools-ocr":               {Ledger: "G-027", Kinds: []string{"L1/extra-node", "L1/unmapped-role"}, Max: 5},
	"app.window.library.detail-tools-ocr-done":          {Ledger: "G-027", Kinds: []string{"L1/extra-node", "L1/unmapped-role"}, Max: 5},
	"app.window.library.detail-tools-tag-suggest":       {Ledger: "G-027", Kinds: []string{"L1/extra-node", "L1/unmapped-role"}, Max: 5},
	// B-071：工具侧栏在 Viva 里排在内容面板之后，渲染层里在它之前——留着内容面板的工具栏时，工具栏与侧栏文字的先后错开，
	// L1 文字顺序整段错位（其余场景把工具栏隐去，见 C-013）；这个场景专门钉住这个差别。
	"app.window.library.detail-tools-note-toolbar": {Ledger: "B-071", Kinds: []string{"L1/extra-node", "L1/missing-node", "L1/text", "L1/unmapped-role", "L2/alignment"}, Max: 23},
}

func checkKnownRed(t *testing.T, sceneID string, pin redPin, rep *parity.Report) {
	t.Helper()
	for _, problem := range knownRedProblems(sceneID, pin, rep) {
		t.Error(problem)
	}
}

// knownRedProblems 返回红场景此刻的失败形态与登记不符的地方；空表示仍是登记的形态（红的、没有表外的类别、没有超条数）。
func knownRedProblems(sceneID string, pin redPin, rep *parity.Report) []string {
	if rep.Pass {
		return []string{fmt.Sprintf("场景 %s 已经通过：请把它从 knownRed 去掉，并关闭台账 %s", sceneID, pin.Ledger)}
	}
	allowed := map[string]bool{}
	for _, k := range pin.Kinds {
		allowed[k] = true
	}
	var problems []string
	for _, is := range rep.Issues {
		if k := string(is.Layer) + "/" + is.Kind; !allowed[k] {
			problems = append(problems, fmt.Sprintf("场景 %s 出现了台账 %s 没有登记的问题类别 [%s]：%s：%s", sceneID, pin.Ledger, k, is.Subject, is.Message))
		}
	}
	if len(rep.Issues) > pin.Max {
		problems = append(problems, fmt.Sprintf("场景 %s 的问题数 %d 超过了台账 %s 登记时的 %d 条", sceneID, len(rep.Issues), pin.Ledger, pin.Max))
	}
	return problems
}

// TestKnownRedPinsTheFailureShape 守住 knownRed 的判定本身：仍是登记的形态才算符合；已经通过、出现表外的类别、
// 超过条数上限，各自都要报出来（否则这张表就成了让红场景溜过 CI 的口子）。
func TestKnownRedPinsTheFailureShape(t *testing.T) {
	pin := redPin{Ledger: "B-000", Kinds: []string{"L2/box", "L2/ink"}, Max: 3}
	issue := func(layer parity.Layer, kind string) parity.Issue {
		return parity.Issue{Layer: layer, Kind: kind, Subject: "s", Message: "m"}
	}
	box, ink, text := issue(parity.LayerL2, "box"), issue(parity.LayerL2, "ink"), issue(parity.LayerL1, "text")
	cases := []struct {
		name string
		rep  parity.Report
		want int
	}{
		{"仍是登记的形态", parity.Report{Issues: []parity.Issue{box, ink, box}}, 0},
		{"刚好到上限", parity.Report{Issues: []parity.Issue{box, box, box}}, 0},
		{"已经通过", parity.Report{Pass: true}, 1},
		{"出现表外的类别", parity.Report{Issues: []parity.Issue{box, text}}, 1},
		{"超过条数上限", parity.Report{Issues: []parity.Issue{box, box, box, box}}, 1},
		{"层对不上（同名类别在别的层）", parity.Report{Issues: []parity.Issue{issue(parity.LayerL1, "box")}}, 1},
		{"表外类别与超条数各报一次", parity.Report{Issues: []parity.Issue{text, text, text, text}}, 5},
	}
	for _, c := range cases {
		if got := knownRedProblems("scene", pin, &c.rep); len(got) != c.want {
			t.Errorf("%s：应报 %d 处，实际 %d 处：%q", c.name, c.want, len(got), got)
		}
	}
}

// TestKnownRedPointsToOpenLedgerItems 守住「红场景必须有一条没关闭的台账条目」：条目已关闭（挪进了「已关闭条目」表）或不存在，
// 场景就不该再留在 knownRed 里；每个 knownRed 的场景也必须是已登记的场景。
func TestKnownRedPointsToOpenLedgerItems(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "docs", "agents", "viva-parity-ledger.md"))
	if err != nil {
		t.Fatal(err)
	}
	ledger := strings.ReplaceAll(string(data), "\r\n", "\n")
	closedFrom := strings.Index(ledger, "### 已关闭条目")
	if closedFrom < 0 {
		t.Fatal("台账里没有「### 已关闭条目」")
	}
	closedTo := len(ledger)
	if next := strings.Index(ledger[closedFrom+1:], "\n## "); next >= 0 {
		closedTo = closedFrom + 1 + next
	}
	registered := map[string]bool{}
	for _, id := range scenes.IDs() {
		registered[id] = true
	}
	for id, pin := range knownRed {
		if !registered[id] {
			t.Errorf("knownRed 里的场景 %s 没有登记", id)
		}
		row := strings.Index(ledger, "| "+pin.Ledger+" |")
		if row < 0 {
			t.Errorf("场景 %s 指向的台账条目 %s 不存在", id, pin.Ledger)
		} else if row >= closedFrom && row < closedTo {
			t.Errorf("场景 %s 指向的台账条目 %s 已关闭：请把场景从 knownRed 去掉", id, pin.Ledger)
		}
	}
}

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
				backdrop, err := scene.Backdrop.Resolve(scene, viva, false)
				if err != nil {
					t.Fatal(err)
				}
				rep, _ := parity.Compare(parity.Inputs{React: react, Viva: viva, FactsOnly: true, Waivers: scene.Waivers, Backdrop: backdrop}, th)
				if pin, red := knownRed[sceneID]; red {
					checkKnownRed(t, sceneID, pin, rep)
					return
				}
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
