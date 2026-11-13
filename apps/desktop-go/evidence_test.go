package main

// JS 值模型（jsvalue.go）与证据脱敏（evidence.go）对 TS 实现的逐字节对拍。
// 先生成基准：pnpm dlx tsx spikes/mygo-db/make-golden-evidence.mts <目录>
// 再运行：GUIZHI_GOLDEN_DIR=<目录> go test -run Evidence ./...

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type evidenceGolden struct {
	JSONCases []struct {
		Input string  `json:"input"`
		Out   *string `json:"out"`
		Error bool    `json:"error"`
	} `json:"jsonCases"`
	URLCases []struct {
		Input json.RawMessage `json:"input"`
		Out   *string         `json:"out"`
		Undef bool            `json:"undef"`
	} `json:"urlCases"`
	TextCases []struct {
		Input json.RawMessage `json:"input"`
		Out   *string         `json:"out"`
		Error bool            `json:"error"`
	} `json:"textCases"`
	TargetCases []struct {
		Input   json.RawMessage `json:"input"`
		OutJSON *string         `json:"outJson"`
		Error   bool            `json:"error"`
	} `json:"targetCases"`
	SanitizeCases []struct {
		JSON    string   `json:"json"`
		Cleared []string `json:"cleared"`
		Out     *string  `json:"out"`
		Error   bool     `json:"error"`
	} `json:"sanitizeCases"`
}

func loadEvidenceGolden(t *testing.T) evidenceGolden {
	t.Helper()
	dir := os.Getenv("GUIZHI_GOLDEN_DIR")
	if dir == "" {
		t.Skip("未设置 GUIZHI_GOLDEN_DIR")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "evidence.json"))
	if err != nil {
		t.Skip("没有 evidence.json，请先运行 make-golden-evidence.mts")
	}
	var g evidenceGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

func mustParse(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	v, err := jsParse(string(raw))
	if err != nil {
		t.Fatalf("基准输入解析失败 %s: %v", raw, err)
	}
	return v
}

func TestEvidenceJSONRoundTrip(t *testing.T) {
	g := loadEvidenceGolden(t)
	for _, c := range g.JSONCases {
		v, err := jsParse(c.Input)
		if c.Error {
			if err == nil {
				t.Errorf("%q: TS 报错，Go 通过", c.Input)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: Go 报错 %v，TS 通过", c.Input, err)
			continue
		}
		got, _ := jsStringify(v)
		if c.Out == nil || got != *c.Out {
			t.Errorf("%q:\nTS: %v\nGo: %s", c.Input, nilStr(c.Out), got)
		}
	}
}

func nilStr(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

func TestEvidenceURL(t *testing.T) {
	g := loadEvidenceGolden(t)
	for _, c := range g.URLCases {
		in := mustParse(t, c.Input)
		got, ok := evidenceURL(in)
		switch {
		case c.Undef && ok:
			t.Errorf("%s: TS 为 undefined，Go 得到 %q", c.Input, got)
		case !c.Undef && (!ok || c.Out == nil || got != *c.Out):
			t.Errorf("%s:\nTS: %v\nGo: %q (ok=%v)", c.Input, nilStr(c.Out), got, ok)
		}
	}
}

func TestEvidenceText(t *testing.T) {
	g := loadEvidenceGolden(t)
	for _, c := range g.TextCases {
		got, err := evidenceText(mustParse(t, c.Input))
		if c.Error {
			if err == nil {
				t.Errorf("%s: TS 报错，Go 通过", c.Input)
			}
			continue
		}
		if err != nil || c.Out == nil || got != *c.Out {
			t.Errorf("%s:\nTS: %v\nGo: %q (%v)", c.Input, nilStr(c.Out), got, err)
		}
	}
}

func TestEvidenceTarget(t *testing.T) {
	g := loadEvidenceGolden(t)
	for _, c := range g.TargetCases {
		v, err := sanitizeEvidenceTarget(mustParse(t, c.Input))
		if c.Error {
			if err == nil {
				t.Errorf("%s: TS 报错，Go 通过", c.Input)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: Go 报错 %v", c.Input, err)
			continue
		}
		got, ok := jsStringify(v)
		if c.OutJSON == nil {
			if ok {
				t.Errorf("%s: TS 为 undefined，Go 得到 %s", c.Input, got)
			}
			continue
		}
		if !ok || got != *c.OutJSON {
			t.Errorf("%s:\nTS: %s\nGo: %s", c.Input, *c.OutJSON, got)
		}
	}
}

func TestEvidenceSanitizeJSON(t *testing.T) {
	g := loadEvidenceGolden(t)
	for i, c := range g.SanitizeCases {
		cleared := map[string]bool{}
		for _, id := range c.Cleared {
			cleared[id] = true
		}
		got, err := sanitizeEvidenceJSON(c.JSON, cleared)
		if c.Error {
			if err == nil {
				t.Errorf("#%d %s: TS 报错，Go 通过 → %s", i, c.JSON, got)
			}
			continue
		}
		if err != nil || c.Out == nil || got != *c.Out {
			t.Errorf("#%d 输入 %s（清除 %v）:\nTS: %s\nGo: %s (%v)", i, c.JSON, c.Cleared, nilStr(c.Out), got, err)
		}
	}
}
