package main

// 原文快照净化差分测试：对拍 golden-websnap.mts 用真实 TS 模块录制的 cleanHtml / cleanCss / sanitizeSnapshot /
// snapshotDocument / snapshotHash / escapeSnapshotText 结果（GUIZHI_WEBSNAP_GOLDEN_DIR 或 GUIZHI_GOLDEN_DIR）。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type wsGoldenText struct {
	Input  string  `json:"input"`
	Output *string `json:"output"`
	Error  *string `json:"error"`
}

type wsGolden struct {
	Allowed []string       `json:"allowed"`
	HTML    []wsGoldenText `json:"html"`
	Inline  []wsGoldenText `json:"inline"`
	Sheet   []wsGoldenText `json:"sheet"`
	Sanit   []struct {
		Name     string          `json:"name"`
		Snapshot json.RawMessage `json:"snapshot"`
		Output   json.RawMessage `json:"output"`
		Error    *string         `json:"error"`
	} `json:"sanitize"`
	Documents []struct {
		Name       string          `json:"name"`
		InstanceID *string         `json:"instanceId"`
		Output     string          `json:"output"`
		Snapshot   json.RawMessage `json:"-"`
	} `json:"documents"`
	Escapes []wsGoldenText `json:"escapes"`
	Hashes  []struct {
		Snapshot json.RawMessage `json:"snapshot"`
		Output   string          `json:"output"`
	} `json:"hashes"`
	Bridge     string `json:"bridge"`
	BridgeHash string `json:"bridgeHash"`
}

func loadWebsnapGolden(t *testing.T) (*wsGolden, map[string]string) {
	t.Helper()
	dir := os.Getenv("GUIZHI_WEBSNAP_GOLDEN_DIR")
	if dir == "" {
		dir = os.Getenv("GUIZHI_GOLDEN_DIR")
	}
	if dir == "" {
		t.Skip("未设置 GUIZHI_WEBSNAP_GOLDEN_DIR")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "golden-websnap.json"))
	if err != nil {
		t.Skip("未找到 golden-websnap.json：" + err.Error())
	}
	var g wsGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	table := map[string]string{}
	for _, u := range g.Allowed {
		table[u] = u
	}
	return &g, table
}

func TestWebsnapCleanGolden(t *testing.T) {
	g, table := loadWebsnapGolden(t)
	failures := 0
	fail := func(format string, args ...any) {
		failures++
		if failures <= 25 {
			t.Errorf(format, args...)
		}
	}
	check := func(kind string, i int, c wsGoldenText, got string, err error) {
		t.Helper()
		if c.Error != nil {
			if err == nil || err.Error() != *c.Error {
				fail("%s[%d] %q：期望错误 %q，得到 %v / %q", kind, i, c.Input, *c.Error, err, got)
			}
			return
		}
		if err != nil {
			fail("%s[%d] %q：意外错误 %v", kind, i, c.Input, err)
			return
		}
		if c.Output == nil || got != *c.Output {
			want := ""
			if c.Output != nil {
				want = *c.Output
			}
			fail("%s[%d] %q：\n got  %q\n want %q", kind, i, c.Input, got, want)
		}
	}
	for i, c := range g.HTML {
		got, err := snapshotCleanHTML(c.Input, table)
		check("html", i, c, got, err)
	}
	for i, c := range g.Inline {
		got, err := snapshotCleanCSS(c.Input, table, false)
		check("inline", i, c, got, err)
	}
	for i, c := range g.Sheet {
		got, err := snapshotCleanCSS(c.Input, table, true)
		check("sheet", i, c, got, err)
	}
	for i, c := range g.Escapes {
		got, err := snapshotEscapeText(c.Input)
		check("escape", i, c, got, err)
	}
}

func TestWebsnapSanitizeGolden(t *testing.T) {
	g, _ := loadWebsnapGolden(t)
	for _, c := range g.Sanit {
		got, err := snapshotSanitize(string(c.Snapshot))
		if c.Error != nil {
			if err == nil || err.Error() != *c.Error {
				t.Errorf("%s：期望错误 %q，得到 %v", c.Name, *c.Error, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s：意外错误 %v", c.Name, err)
			continue
		}
		var a, b any
		if err := json.Unmarshal([]byte(got), &a); err != nil {
			t.Errorf("%s：输出不是 JSON：%v", c.Name, err)
			continue
		}
		_ = json.Unmarshal(c.Output, &b)
		ga, _ := json.Marshal(a)
		gb, _ := json.Marshal(b)
		if string(ga) != string(gb) {
			t.Errorf("%s 净化结果不一致：\n got  %.300s\n want %.300s", c.Name, ga, gb)
		}
	}
	for i, h := range g.Hashes {
		got, err := snapshotHashOf(string(h.Snapshot))
		if err != nil || got != h.Output {
			t.Errorf("hash[%d]：got %q (%v) want %q", i, got, err, h.Output)
		}
	}
}

func TestWebsnapBridgeAndDocuments(t *testing.T) {
	g, _ := loadWebsnapGolden(t)
	e, err := getWebsnapEngine()
	if err != nil {
		t.Fatal(err)
	}
	if got := e.gz.Get("bridge").String(); got != g.Bridge {
		t.Errorf("桥接脚本与 TS 不一致（长度 %d vs %d）", len(got), len(g.Bridge))
	}
	// 文档用例按名称取快照输入（同 Sanit 里的快照）
	snaps := map[string]json.RawMessage{}
	for _, c := range g.Sanit {
		snaps[c.Name] = c.Snapshot
	}
	for _, d := range g.Documents {
		inst := ""
		if d.InstanceID != nil {
			inst = *d.InstanceID
		}
		got, err := snapshotDocumentOf(string(snaps[d.Name]), inst)
		if err != nil {
			t.Errorf("%s/%q：%v", d.Name, inst, err)
			continue
		}
		if got != d.Output {
			t.Errorf("%s/%q 文档不一致：\n got  %.200s\n want %.200s", d.Name, inst, got, d.Output)
		}
	}
}
