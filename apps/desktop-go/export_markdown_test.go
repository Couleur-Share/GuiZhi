package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// TestExportMarkdownParity：同一份库、同一批资产，Go 导出的目录树与 TS 逐字节一致。
func TestExportMarkdownParity(t *testing.T) {
	dir := os.Getenv("GUIZHI_GOLDEN_DIR")
	if dir == "" {
		t.Skip("未设置 GUIZHI_GOLDEN_DIR")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		ExportMarkdown struct {
			Stats struct{ Count, AssetCount int }
			Tree  map[string]string
		}
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	if len(g.ExportMarkdown.Tree) == 0 {
		t.Fatal("golden 里没有 exportMarkdown，请重新生成基准")
	}

	l := openWriteCopy(t, dir, "export-base.db")
	userData := t.TempDir()
	l.paths = Paths{UserData: userData}
	if err := os.CopyFS(filepath.Join(userData, "data", "assets"), os.DirFS(filepath.Join(dir, "export-assets"))); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	count, assets, err := l.exportKnowledgeToMarkdown(context.Background(), out)
	if err != nil {
		t.Fatal(err)
	}
	if count != g.ExportMarkdown.Stats.Count || assets != g.ExportMarkdown.Stats.AssetCount {
		t.Errorf("统计不一致：Go %d/%d，TS %d/%d", count, assets, g.ExportMarkdown.Stats.Count, g.ExportMarkdown.Stats.AssetCount)
	}
	got := map[string]string{}
	_ = filepath.WalkDir(out, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, _ := os.ReadFile(p)
		rel, _ := filepath.Rel(out, p)
		got[filepath.ToSlash(rel)] = base64.StdEncoding.EncodeToString(b)
		return nil
	})
	if !reflect.DeepEqual(got, g.ExportMarkdown.Tree) {
		var names []string
		for n := range g.ExportMarkdown.Tree {
			names = append(names, n)
		}
		for n := range got {
			if _, ok := g.ExportMarkdown.Tree[n]; !ok {
				names = append(names, n)
			}
		}
		sort.Strings(names)
		for _, n := range names {
			w, wok := g.ExportMarkdown.Tree[n]
			gt, gok := got[n]
			switch {
			case !wok:
				t.Errorf("多出文件：%q", n)
			case !gok:
				t.Errorf("缺少文件：%q", n)
			case w != gt:
				wb, _ := base64.StdEncoding.DecodeString(w)
				gb, _ := base64.StdEncoding.DecodeString(gt)
				t.Errorf("内容不一致：%q\nTS: %q\nGo: %q", n, wb, gb)
			}
		}
	}
}

func TestExportAIHandoff(t *testing.T) {
	l := &Library{}
	b := &Bridge{handlers: map[string]handler{}}
	l.registerExport(b)
	call := func(arg string) map[string]any {
		t.Helper()
		v, err := b.handlers["export:aiHandoff"](context.Background(), []json.RawMessage{json.RawMessage(arg)})
		if err != nil {
			return map[string]any{"throw": err.Error()}
		}
		return v.(map[string]any)
	}
	if r := call(`{"text":"   "}`); r["error"] != "交接稿内容为空" {
		t.Errorf("空文本：%v", r)
	}
	if r := call(`null`); r["error"] != "交接稿内容为空" {
		t.Errorf("null 请求：%v", r)
	}
	if r := call(`{"text":"x","title":5}`); r["throw"] == nil {
		t.Errorf("非字符串标题应抛错：%v", r)
	}
	if r := call(`{"text":"x"}`); r["canceled"] != true {
		t.Errorf("无对话框应视为取消：%v", r)
	}
	var gotDefault string
	file := filepath.Join(t.TempDir(), "out.md")
	l.saveFileDialog = func(title, defaultPath string, filters []dialogFilter) (string, error) {
		gotDefault = filepath.Base(defaultPath)
		return file, nil
	}
	if r := call(`{"text":"正文\ud800","title":" a/b:c "}`); r["success"] != true || r["filePath"] != file {
		t.Errorf("保存失败：%v", r)
	}
	if gotDefault != "a b c.md" {
		t.Errorf("默认文件名 %q", gotDefault)
	}
	if b, _ := os.ReadFile(file); string(b) != "正文\ufffd" {
		t.Errorf("文件内容 %q（孤立代理项应变成 U+FFFD）", b)
	}
	l.saveFileDialog = func(string, string, []dialogFilter) (string, error) { return "", nil }
	if r := call(`{"text":"x","title":""}`); r["canceled"] != true {
		t.Errorf("取消：%v", r)
	}
	l.saveFileDialog = func(string, string, []dialogFilter) (string, error) {
		return filepath.Join(t.TempDir(), "no", "dir.md"), nil
	}
	if r := call(`{"text":"x"}`); r["success"] != false || r["error"] == nil {
		t.Errorf("写入失败应带原因：%v", r)
	}
}
