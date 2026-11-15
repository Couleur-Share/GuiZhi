package settings

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"testing"
)

// golden-settings.json 由 TS 实现导出（apps/desktop/tests/unit/stores/settings-native-golden.test.ts）：
// 同一批输入经渲染层真实的 migrate/merge 与 ui-storage 合并后，得到的结果。这里逐项核对 Go 的移植。
// 两边谁变了都会红：TS 变了，那边的测试先失败；Go 变了，这里失败。

type goldenCase struct {
	Name    string                     `json:"name"`
	Version int                        `json:"version"`
	State   map[string]json.RawMessage `json:"state"`
	Expect  map[string]json.RawMessage `json:"expect"`
}

type goldenFile struct {
	GoKeys        []string     `json:"goKeys"`
	UIStorageKeys []string     `json:"uiStorageKeys"`
	Settings      []goldenCase `json:"settings"`
	UIStorage     []goldenCase `json:"uiStorage"`
}

func loadGolden(t *testing.T) goldenFile {
	t.Helper()
	raw, err := os.ReadFile("testdata/golden-settings.json")
	if err != nil {
		t.Fatalf("读取 golden 失败（由 TS 测试 UPDATE_GOLDEN=1 导出）：%v", err)
	}
	var g goldenFile
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	if len(g.Settings) < 50 || len(g.UIStorage) < 15 {
		t.Fatalf("golden 语料过少：settings=%d ui-storage=%d", len(g.Settings), len(g.UIStorage))
	}
	return g
}

func decodeAny(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("解码 %s：%v", raw, err)
	}
	return v
}

// notImported 是 Go 侧故意不从旧数据导入的字段：可执行文件路径只接受文件选择器返回过的值。
var notImported = map[string]bool{"ytDlpPath": true, "ffmpegPath": true}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// 键集合必须与 Go 的 UI 与 App 结构体一致：加了字段却没进 golden，这里先红。
func TestGoldenKeysMatchStructFields(t *testing.T) {
	g := loadGolden(t)
	want := map[string]bool{}
	ui, err := Keys(DefaultUI())
	if err != nil {
		t.Fatal(err)
	}
	app, err := Keys(DefaultApp())
	if err != nil {
		t.Fatal(err)
	}
	for k := range ui {
		want[k] = true
	}
	for k := range app {
		if k != "networkProxy" && k != "flareSolverr" { // 这两项的归一化规则由 Go 侧 normalizeNetworkProxy/validateFlareSolverr 把关，另有 golden
			want[k] = true
		}
	}
	got := map[string]bool{}
	for _, k := range g.GoKeys {
		got[k] = true
	}
	if !reflect.DeepEqual(want, got) {
		var missing, extra []string
		for k := range want {
			if !got[k] {
				missing = append(missing, k)
			}
		}
		for k := range got {
			if !want[k] {
				extra = append(extra, k)
			}
		}
		sort.Strings(missing)
		sort.Strings(extra)
		t.Fatalf("golden 的 goKeys 与 Go 结构体不一致：golden 缺 %v，多出 %v（改 TS 测试的 GO_KEYS 后重新导出）", missing, extra)
	}
}

func TestSettingsMatchTypeScriptGolden(t *testing.T) {
	g := loadGolden(t)
	for _, c := range g.Settings {
		t.Run(c.Name, func(t *testing.T) {
			wrapped := mustJSON(t, map[string]any{"state": c.State, "version": c.Version})
			leg := ParseLegacy(map[string]string{LegacySettingsKey: wrapped})
			if !leg.HasSettings {
				t.Fatalf("没有解出设置：%v", leg.Issues)
			}
			got, err := Keys(leg.UI)
			if err != nil {
				t.Fatal(err)
			}
			// 旧数据没给的 Go 侧键取默认值（旧实现里是 merge 到默认值上）
			app, err := Keys(DefaultApp())
			if err != nil {
				t.Fatal(err)
			}
			for k, v := range app {
				got[k] = v
			}
			for k, v := range leg.App {
				got[k] = v
			}
			for _, k := range g.GoKeys {
				if notImported[k] {
					if _, ok := leg.App[k]; ok {
						t.Errorf("%s 不应从旧数据导入", k)
					}
					continue
				}
				want, ok := c.Expect[k]
				if !ok {
					t.Fatalf("golden 缺少 %s", k)
				}
				have, ok := got[k]
				if !ok {
					t.Errorf("%s：Go 没有产出", k)
					continue
				}
				if !reflect.DeepEqual(decodeAny(t, want), decodeAny(t, have)) {
					t.Errorf("%s：TS=%s Go=%s", k, want, have)
				}
			}
		})
	}
}

func TestUIStorageMatchesTypeScriptGolden(t *testing.T) {
	g := loadGolden(t)
	defaults := map[string]any{
		"appModule": "library", "isSidebarCollapsed": false,
		"sidebarPanelWidth": 288.0, "itemListPaneWidth": 320.0, "wikiCatalogPaneWidth": 300.0, "libraryViewMode": "card",
	}
	prefKey := map[string]string{
		"appModule": PrefAppModule, "isSidebarCollapsed": PrefSidebarCollapsed, "sidebarPanelWidth": PrefSidebarPanelWidth,
		"itemListPaneWidth": PrefItemListPaneWidth, "wikiCatalogPaneWidth": PrefWikiCatalogPaneWidth, "libraryViewMode": PrefLibraryViewMode,
	}
	for _, c := range g.UIStorage {
		t.Run(c.Name, func(t *testing.T) {
			wrapped := mustJSON(t, map[string]any{"state": c.State, "version": 0})
			leg := ParseLegacy(map[string]string{"ui-storage": wrapped})
			for _, k := range g.UIStorageKeys {
				var have any = defaults[k] // 缺省项由使用处取默认值
				if raw, ok := leg.Prefs[prefKey[k]]; ok {
					have = decodeAny(t, raw)
				}
				if want := decodeAny(t, c.Expect[k]); !reflect.DeepEqual(want, have) {
					t.Errorf("%s：TS=%v Go=%v", k, want, have)
				}
			}
		})
	}
}
