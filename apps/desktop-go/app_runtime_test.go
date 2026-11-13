package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

type appRuntimeGolden struct {
	UserData string           `json:"userData"`
	Home     string           `json:"home"`
	SrcDir   string           `json:"srcDir"`
	Paths    map[string]any   `json:"paths"`
	Seed     map[string][]int `json:"seedFiles"`
	Src      map[string][]int `json:"srcFiles"`
	Reads    []map[string]any `json:"reads"`
	SaveOps  []map[string]any `json:"saveOps"`
	Saves    []map[string]any `json:"saves"`
	Files    []struct {
		Name  string `json:"name"`
		Bytes []int  `json:"bytes"`
	} `json:"files"`
	LogEntries []any            `json:"logEntries"`
	LogLines   []map[string]any `json:"logLines"`
}

func writeBytes(t *testing.T, path string, b []int) {
	t.Helper()
	data := make([]byte, len(b))
	for i, v := range b {
		data[i] = byte(v)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

var uuidRe = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

func TestAppRuntimeParity(t *testing.T) {
	dir := os.Getenv("GUIZHI_GOLDEN_DIR")
	if dir == "" {
		t.Skip("未设置 GUIZHI_GOLDEN_DIR")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var wrap struct {
		AppRuntime appRuntimeGolden `json:"appRuntime"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		t.Fatal(err)
	}
	g := wrap.AppRuntime

	ud := filepath.Join(t.TempDir(), "GuiZhi")
	srcDir := filepath.Join(t.TempDir(), "src")
	imagesDir := Paths{UserData: ud}.ImagesDir()
	_ = os.MkdirAll(filepath.Join(imagesDir, "dir"), 0o755)
	_ = os.MkdirAll(srcDir, 0o755)
	for n, b := range g.Seed {
		writeBytes(t, filepath.Join(imagesDir, n), b)
	}
	for n, b := range g.Src {
		writeBytes(t, filepath.Join(srcDir, n), b)
	}

	l := &Library{paths: Paths{UserData: ud}}
	var queue [][]string
	l.openDialog = func(bool, string, []string) ([]string, error) {
		next := queue[0]
		queue = queue[1:]
		return next, nil
	}
	b := &Bridge{handlers: map[string]handler{}, sends: map[string]func([]json.RawMessage){}, seen: map[string]int{}, missing: map[string]int{}}
	l.registerAppRuntime(b)
	invoke := func(channel string, args ...any) map[string]any {
		var list []json.RawMessage
		for _, a := range args {
			bs, _ := json.Marshal(a)
			list = append(list, bs)
		}
		res, err := b.handlers[channel](context.Background(), list)
		if err != nil {
			return map[string]any{"error": err.Error()}
		}
		bs, _ := json.Marshal(res)
		var v any
		_ = json.Unmarshal(bs, &v)
		return map[string]any{"value": v}
	}
	// 只比较「是否报错」，不比较报错文案
	errOnly := func(m map[string]any) any {
		if _, ok := m["error"]; ok {
			return "ERR"
		}
		return m["value"]
	}
	rel := func(v any, root string) any {
		bs, _ := json.Marshal(v)
		s := strings.ReplaceAll(string(bs), strings.ReplaceAll(root, `\`, `\\`), "<UD>")
		var out any
		_ = json.Unmarshal([]byte(s), &out)
		return out
	}

	t.Run("paths", func(t *testing.T) {
		want := rel(map[string]any{"value": g.Paths["value"]}, g.UserData)
		got := rel(map[string]any{"value": invoke("app:getRuntimePaths")["value"]}, ud)
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("路径不一致\nTS: %v\nGo: %v", want, got)
		}
	})

	t.Run("readBase64", func(t *testing.T) {
		for i, r := range g.Reads {
			got := errOnly(invoke("image:readBase64", r["arg"]))
			want := errOnly(r)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("#%d %v: TS=%v Go=%v", i, r["arg"], want, got)
			}
		}
	})

	t.Run("save", func(t *testing.T) {
		// 规范化：把源目录换成 <SRC>，把随机文件名按出现顺序换成 IMGn（TS 与 Go 各用一张表）
		canon := func(names map[string]string, root string) func(any) any {
			return func(v any) any {
				bs, _ := json.Marshal(v)
				s := strings.ReplaceAll(string(bs), strings.ReplaceAll(root, `\`, `\\`), "<SRC>")
				s = strings.ReplaceAll(s, strings.ReplaceAll(root, `\`, `/`), "<SRC>")
				s = strings.ReplaceAll(s, "<SRC>"+`\\`, "<SRC>/")
				s = uuidRe.ReplaceAllStringFunc(s, func(u string) string {
					if _, ok := names[u]; !ok {
						names[u] = fmt.Sprintf("IMG%d", len(names)+1)
					}
					return names[u]
				})
				var out any
				_ = json.Unmarshal([]byte(s), &out)
				return out
			}
		}
		tsCanon := canon(map[string]string{}, g.SrcDir)
		goCanon := canon(map[string]string{}, srcDir)
		swap := func(v any) any { // 把操作里的 TS 源目录换成 Go 的
			bs, _ := json.Marshal(v)
			s := strings.ReplaceAll(string(bs), strings.ReplaceAll(g.SrcDir, `\`, `\\`), strings.ReplaceAll(srcDir, `\`, `\\`))
			s = strings.ReplaceAll(s, strings.ReplaceAll(g.SrcDir, `\`, `/`), strings.ReplaceAll(srcDir, `\`, `/`))
			var out any
			_ = json.Unmarshal([]byte(s), &out)
			return out
		}
		for i, step := range g.Saves {
			op := step["op"].(map[string]any)
			var got map[string]any
			if sel, ok := op["select"]; ok {
				if s, isStr := sel.(string); isStr && s == "cancel" {
					queue = append(queue, nil)
				} else {
					var paths []string
					for _, p := range swap(sel).([]any) {
						paths = append(paths, p.(string))
					}
					queue = append(queue, paths)
				}
				got = invoke("dialog:selectImage")
			} else {
				got = invoke("image:save", swap(op["save"]))
			}
			want, have := tsCanon(errOnly(step)), goCanon(errOnly(got))
			if !reflect.DeepEqual(want, have) {
				t.Errorf("步骤 %d %v\nTS: %v\nGo: %v", i, op, want, have)
			}
		}
		// 目录内容：按归一化后的名字与字节比较
		entries, _ := os.ReadDir(imagesDir)
		var gotFiles, wantFiles []string
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			data, _ := os.ReadFile(filepath.Join(imagesDir, e.Name()))
			gotFiles = append(gotFiles, fmt.Sprintf("%v|%v", goCanon(e.Name()), data))
		}
		for _, f := range g.Files {
			data := make([]byte, len(f.Bytes))
			for i, v := range f.Bytes {
				data[i] = byte(v)
			}
			wantFiles = append(wantFiles, fmt.Sprintf("%v|%v", tsCanon(f.Name), data))
		}
		sort.Strings(gotFiles)
		sort.Strings(wantFiles)
		if !reflect.DeepEqual(gotFiles, wantFiles) {
			t.Fatalf("图片目录不一致\nTS: %v\nGo: %v", wantFiles, gotFiles)
		}
	})
	t.Run("log", func(t *testing.T) {
		// 日志脱敏会把用户主目录替换成 ~。golden 里的消息带的是生成基准时的主目录（已统一改写为 C:\Users\user），
		// 所以这里固定主目录，不依赖运行机器的用户名；否则换一台机器（例如 CI）就不会触发脱敏。
		t.Setenv("USERPROFILE", `C:\Users\user`)
		t.Setenv("HOME", `C:\Users\user`)
		send := b.sends["log:appError"]
		send(nil)
		for _, e := range g.LogEntries {
			bs, _ := json.Marshal(e)
			send([]json.RawMessage{bs})
		}
		data, _ := os.ReadFile(filepath.Join(ud, "logs", "error.log"))
		var got []map[string]any
		for _, line := range strings.Split(string(data), "\n") {
			if line == "" {
				continue
			}
			var r map[string]any
			if err := json.Unmarshal([]byte(line), &r); err != nil {
				t.Fatalf("日志行不是合法 JSON：%v", err)
			}
			for _, k := range []string{"ts", "pid", "version"} {
				if _, ok := r[k]; !ok {
					t.Fatalf("缺少字段 %s", k)
				}
				delete(r, k)
			}
			got = append(got, r)
		}
		if len(got) != len(g.LogLines) {
			t.Fatalf("日志条数 TS=%d Go=%d", len(g.LogLines), len(got))
		}
		for i := range got {
			if !reflect.DeepEqual(got[i], g.LogLines[i]) {
				bg, _ := json.Marshal(got[i])
				bw, _ := json.Marshal(g.LogLines[i])
				t.Errorf("日志 #%d\nTS: %s\nGo: %s", i, bw, bg)
			}
		}
	})
}

func toGoMap(m map[string]any, f func(any) any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = f(v)
	}
	return out
}

func TestLogRotation(t *testing.T) {
	p := Paths{UserData: t.TempDir()}
	for i := 0; i < 400; i++ {
		p.logAppError("s", "a", fmt.Sprintf("%04d:%s", i, strings.Repeat("x", 2000)), nil)
	}
	data, _ := os.ReadFile(filepath.Join(p.LogsDir(), "error.log"))
	if len(data) > diagMaxLogBytes+4096 {
		t.Fatalf("日志没有被截断：%d 字节", len(data))
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	var first, last map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("截断后的第一行不完整：%v", err)
	}
	_ = json.Unmarshal([]byte(lines[len(lines)-1]), &last)
	if !strings.HasPrefix(last["message"].(string), "0399:") {
		t.Fatalf("最新一条丢了：%v", last["message"])
	}
}
