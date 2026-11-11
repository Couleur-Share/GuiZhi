package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"testing"
)

type settingsGolden struct {
	Settings struct {
		Defaults json.RawMessage `json:"defaults"`
		DumpSQL  string          `json:"dumpSql"`
		Runs     map[string]struct {
			AI      *string `json:"ai"`
			AIAfter *string `json:"aiAfter"`
			Ops     []struct {
				Fn   string            `json:"fn"`
				Args []json.RawMessage `json:"args"`
			} `json:"ops"`
			Results []struct {
				Value json.RawMessage `json:"value"`
				Error *string         `json:"error"`
			} `json:"results"`
			Dump [][]any `json:"dump"`
		} `json:"runs"`
	} `json:"settings"`
}

// settings 表的值是 JSON 文本：按解析后的值比较（缩进、键序不属于契约），解析不了的原样比较。
func parseSettingsDump(rows [][]any) map[string]any {
	out := map[string]any{}
	for _, r := range rows {
		key, _ := r[0].(string)
		raw, _ := r[1].(string)
		var v any
		if json.Unmarshal([]byte(raw), &v) != nil {
			v = "RAW:" + raw
		}
		out[key] = v
	}
	return out
}

func loadSettingsGolden(t *testing.T) (settingsGolden, string) {
	t.Helper()
	dir := os.Getenv("GUIZHI_GOLDEN_DIR")
	if dir == "" {
		t.Skip("未设置 GUIZHI_GOLDEN_DIR")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g settingsGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	if len(g.Settings.Runs) == 0 {
		t.Fatal("golden 里没有 settings，请重新生成基准")
	}
	return g, dir
}

func TestSettingsDefaultsMatchTS(t *testing.T) {
	g, _ := loadSettingsGolden(t)
	if !reflect.DeepEqual(rawToAny(t, g.Settings.Defaults), normalize(t, defaultSettings())) {
		t.Fatalf("defaultSettings 与 TS 的 DEFAULT_SETTINGS 不一致\nTS: %s", g.Settings.Defaults)
	}
}

func TestSettingsParity(t *testing.T) {
	g, dir := loadSettingsGolden(t)
	names := make([]string, 0, len(g.Settings.Runs))
	for n := range g.Settings.Runs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		run := g.Settings.Runs[name]
		t.Run(name, func(t *testing.T) {
			lib := openWriteCopy(t, dir)
			lib.configDir = filepath.Join(t.TempDir(), "config")
			if run.AI != nil {
				if err := os.MkdirAll(lib.configDir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(lib.aiConfigPath(), []byte(*run.AI), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			b := &Bridge{handlers: map[string]handler{}}
			lib.registerSettings(b)
			for i, op := range run.Ops {
				got, err := b.handlers["settings:"+op.Fn](context.Background(), op.Args)
				want := run.Results[i]
				if want.Error != nil {
					if err == nil {
						t.Errorf("op %d: 期望错误 %q，得到成功", i, *want.Error)
					} else if strictErrorMessage(*want.Error) && err.Error() != *want.Error {
						t.Errorf("op %d: 期望错误 %q，得到 %q", i, *want.Error, err.Error())
					}
					continue
				}
				if err != nil {
					t.Errorf("op %d: %v", i, err)
					continue
				}
				if w, h := rawToAny(t, want.Value), normalize(t, got); !reflect.DeepEqual(w, h) {
					wj, _ := json.MarshalIndent(w, "", " ")
					hj, _ := json.MarshalIndent(h, "", " ")
					t.Errorf("op %d 返回值不一致\nTS: %s\nGo: %s", i, wj, hj)
				}
			}
			compareAIFile(t, lib, run.AIAfter)
			rows, err := lib.db.Query(context.Background(), g.Settings.DumpSQL)
			if err != nil {
				t.Fatal(err)
			}
			w, h := parseSettingsDump(run.Dump), parseSettingsDump(rows.Values)
			if !reflect.DeepEqual(normalize(t, w), normalize(t, h)) {
				wj, _ := json.MarshalIndent(w, "", " ")
				hj, _ := json.MarshalIndent(h, "", " ")
				t.Errorf("settings 表不一致\nTS: %s\nGo: %s", wj, hj)
			}
		})
	}
}

// strictErrorMessage：含中文的是我们自己定义的提示，必须逐字一致；
// 纯英文的是 JS 引擎或 JSON.parse 的原文（措辞随引擎版本变化），只要求「确实报错」。
// 例外：Object.keys(null) 的提示我们刻意对齐。
func strictErrorMessage(msg string) bool {
	if msg == "Cannot convert undefined or null to object" {
		return true
	}
	for _, r := range msg {
		if r > 127 {
			return true
		}
	}
	return false
}

var updatedAtLine = regexp.MustCompile(`"updatedAt": "[^"]*"`)

// compareAIFile 比较操作后的 ai-models.json 原文（updatedAt 取运行时间，先抹平）。
func compareAIFile(t *testing.T, lib *Library, want *string) {
	t.Helper()
	raw, err := os.ReadFile(lib.aiConfigPath())
	if want == nil {
		if err == nil {
			t.Errorf("不应产生 ai-models.json，实际内容:\n%s", raw)
		}
		return
	}
	if err != nil {
		t.Errorf("缺少 ai-models.json: %v", err)
		return
	}
	mask := func(s string) string { return updatedAtLine.ReplaceAllString(s, `"updatedAt": "T"`) }
	if mask(string(raw)) != mask(*want) {
		t.Errorf("ai-models.json 不一致\nTS:\n%s\nGo:\n%s", *want, raw)
	}
}
