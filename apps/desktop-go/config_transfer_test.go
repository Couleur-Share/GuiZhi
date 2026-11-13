package main

// 配置迁移与快捷键差分测试：golden.shortcuts / golden.configTransfer 由 TS 真实实现
// （shortcuts.ts、config-transfer.ipc.ts、services/config-transfer）在隔离 userData 里生成。

import (
	"bytes"
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

type cfgGolden struct {
	Shortcuts struct {
		Scenarios []scGoldenScenario `json:"scenarios"`
	} `json:"shortcuts"`
	ConfigTransfer struct {
		Seeds         map[string]map[string]string `json:"seeds"`
		Files         map[string]string            `json:"files"`
		BuiltinStyles json.RawMessage              `json:"builtinStyles"`
		Scenarios     []ctGoldenScenario           `json:"scenarios"`
	} `json:"configTransfer"`
}

// rawOp 保留 golden 里参数的原始 JSON，这样对象键序与渲染进程发来的一致。
type rawOp map[string]json.RawMessage

func (o rawOp) str(k string) string {
	var s string
	if raw, ok := o[k]; ok {
		_ = json.Unmarshal(raw, &s)
	}
	return s
}

func (o rawOp) has(k string) bool { _, ok := o[k]; return ok }

// notNull 表示键存在且不是 JSON null。
func (o rawOp) notNull(k string) bool {
	raw, ok := o[k]
	return ok && string(raw) != "null"
}

type envDump struct {
	Config map[string]string `json:"config"`
	Legacy map[string]string `json:"legacy"`
}

type scGoldenScenario struct {
	Name    string            `json:"name"`
	Config  map[string]string `json:"config"`
	Legacy  map[string]string `json:"legacy"`
	Ops     []rawOp           `json:"ops"`
	Results []any             `json:"results"`
	After   envDump           `json:"after"`
}

type ctGoldenScenario struct {
	Name    string           `json:"name"`
	Seed    string           `json:"seed"`
	Ops     []rawOp          `json:"ops"`
	Results []map[string]any `json:"results"`
	After   envDump          `json:"after"`
}

func loadCfgGolden(t *testing.T) cfgGolden {
	t.Helper()
	dir := os.Getenv("GUIZHI_GOLDEN_DIR")
	if dir == "" {
		t.Skip("未设置 GUIZHI_GOLDEN_DIR")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g cfgGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

var cfgStampRe = regexp.MustCompile(`\d{8}-\d{6}`)

func cfgNormName(name string) string {
	return regexp.MustCompile(`pre-import-(\d{8}-\d{6})`).ReplaceAllStringFunc(name, func(m string) string {
		if m[len("pre-import-"):] >= "20250101-000000" {
			return "pre-import-NEW"
		}
		return m
	})
}

func dumpDirTree(dir string) map[string]string {
	tree := map[string]string{}
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		data, _ := os.ReadFile(p)
		rel, _ := filepath.Rel(dir, p)
		tree[cfgNormName(filepath.ToSlash(rel))] = string(data)
		return nil
	})
	return tree
}

var updatedAtRe = regexp.MustCompile(`"updatedAt": "[^"]*"`)

func normTreeText(tree map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range tree {
		out[k] = updatedAtRe.ReplaceAllString(v, `"updatedAt": "NOW"`)
	}
	return out
}

func writeSeedFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, text := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func dumpEnvFiles(userData, cfgDir string) envDump {
	d := envDump{Config: normTreeText(dumpDirTree(cfgDir)), Legacy: map[string]string{}}
	for _, n := range []string{"shortcuts.json", "shortcut-mode.json"} {
		if data, err := os.ReadFile(filepath.Join(userData, n)); err == nil {
			d.Legacy[n] = string(data)
		}
	}
	return d
}

func equalEnv(t *testing.T, label string, want, got envDump) {
	t.Helper()
	for _, part := range []struct {
		name string
		w, g map[string]string
	}{{"config", want.Config, got.Config}, {"legacy", want.Legacy, got.Legacy}} {
		var names []string
		seen := map[string]bool{}
		for n := range part.w {
			names = append(names, n)
			seen[n] = true
		}
		for n := range part.g {
			if !seen[n] {
				names = append(names, n)
			}
		}
		sort.Strings(names)
		for _, n := range names {
			w, wok := part.w[n]
			g, gok := part.g[n]
			switch {
			case wok && !gok:
				t.Errorf("%s: %s/%s 缺失（TS 有）", label, part.name, n)
			case !wok && gok:
				t.Errorf("%s: %s/%s 多出（TS 没有）\n%.300s", label, part.name, n, g)
			case w != g:
				t.Errorf("%s: %s/%s 内容不一致\nTS: %.600q\nGo: %.600q", label, part.name, n, w, g)
			}
		}
	}
}

// jsonRoundTrip 把 Go 返回值转成与 golden 同形的 any（omap 走 MarshalJSON，数字都是 float64）。
func jsonRoundTrip(t *testing.T, v any) any {
	t.Helper()
	raw, err := marshalNoEscape(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func opArgs(t *testing.T, op rawOp, keys ...string) []json.RawMessage {
	t.Helper()
	var out []json.RawMessage
	for _, k := range keys {
		if raw, ok := op[k]; ok {
			out = append(out, raw)
		} else {
			out = append(out, json.RawMessage("null"))
		}
	}
	for len(out) > 0 && !op.has(keys[len(out)-1]) {
		out = out[:len(out)-1]
	}
	return out
}

func callHandler(t *testing.T, b *Bridge, ch string, args []json.RawMessage) any {
	t.Helper()
	v, err := b.handlers[ch](context.Background(), args)
	if err != nil {
		return map[string]any{"error": true}
	}
	return map[string]any{"value": jsonRoundTrip(t, v)}
}

// 错误只比「有没有」，不比文本（与 TS 的异常对象不同形）。
func sameOutcome(want, got any) bool {
	w, _ := want.(map[string]any)
	g, _ := got.(map[string]any)
	if _, ok := w["error"]; ok {
		_, gok := g["error"]
		return gok
	}
	return reflect.DeepEqual(want, got)
}

func TestShortcutsParity(t *testing.T) {
	g := loadCfgGolden(t)
	for _, sc := range g.Shortcuts.Scenarios {
		t.Run(sc.Name, func(t *testing.T) {
			ud := t.TempDir()
			cfg := filepath.Join(ud, "config")
			writeSeedFiles(t, cfg, sc.Config)
			writeSeedFiles(t, ud, sc.Legacy)
			l := &Library{configDir: cfg, paths: Paths{UserData: ud}}
			b := &Bridge{handlers: map[string]handler{}, sends: map[string]func([]json.RawMessage){}}
			l.registerShortcutsIPC(b)
			for i, op := range sc.Ops {
				var got any
				switch op.str("op") {
				case "load":
					l.startShortcuts(true)
					got = nil
				case "get":
					got = callHandler(t, b, chShortcutsGet, nil)
				case "getMode":
					got = callHandler(t, b, chShortcutsGetMode, nil)
				case "set":
					got = callHandler(t, b, chShortcutsSet, opArgs(t, op, "arg"))
				case "setMode":
					b.sends[chShortcutsSetMode](opArgs(t, op, "arg"))
					got = map[string]any{"value": nil}
				case "persist":
					acc, _ := parseOrdered(op["accelerators"])
					modes, _ := parseOrdered(op["modes"])
					am, _ := acc.(*omap)
					mm, _ := modes.(*omap)
					if err := l.persistImportedShortcuts(am, mm); err != nil {
						got = map[string]any{"error": err.Error()}
					} else {
						got = map[string]any{"value": nil}
					}
				}
				if !sameOutcome(sc.Results[i], got) {
					t.Errorf("op %d %v：\n  TS: %v\n  Go: %v", i, op.str("op"), sc.Results[i], got)
				}
			}
			equalEnv(t, sc.Name, sc.After, dumpEnvFiles(ud, cfg))
		})
	}
}

func parseOrderedAny(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return parseOrdered(raw)
}

func TestIllustrationBuiltinParity(t *testing.T) {
	g := loadCfgGolden(t)
	var a, b bytes.Buffer
	if err := json.Compact(&a, g.ConfigTransfer.BuiltinStyles); err != nil {
		t.Fatal(err)
	}
	if err := json.Compact(&b, illustrationBuiltinJSON); err != nil {
		t.Fatal(err)
	}
	if a.String() != b.String() {
		t.Fatal("内置配图风格与 TS 不一致：请重新运行 spikes/mygo-db/dump-builtin-styles.mts 生成 illustration_builtin.json")
	}
}

// ── 配置迁移 ──

type ctRun struct {
	l         *Library
	b         *Bridge
	userData  string
	cfgDir    string
	ctDir     string
	pickNext  []string
	saveNext  []string
	saveCalls []map[string]any
}

func newCtRun(t *testing.T, seed map[string]string) *ctRun {
	t.Helper()
	ud := t.TempDir()
	r := &ctRun{userData: ud, cfgDir: filepath.Join(ud, "config"), ctDir: filepath.Join(ud, "ct")}
	os.MkdirAll(r.ctDir, 0o755)
	writeSeedFiles(t, r.cfgDir, seed)
	r.l = &Library{configDir: r.cfgDir, paths: Paths{UserData: ud}}
	r.l.pickFiles = func(multiple bool, filters []dialogFilter) ([]string, error) {
		if len(r.pickNext) == 0 {
			return nil, nil
		}
		p := r.pickNext[0]
		r.pickNext = r.pickNext[1:]
		if p == "" {
			return nil, nil
		}
		return []string{p}, nil
	}
	r.l.saveFileDialog = func(title, defaultPath string, filters []dialogFilter) (string, error) {
		var fs []any
		for _, f := range filters {
			exts := make([]any, len(f.Exts))
			for i, e := range f.Exts {
				exts[i] = e
			}
			fs = append(fs, map[string]any{"name": f.Name, "extensions": exts})
		}
		r.saveCalls = append(r.saveCalls, map[string]any{"title": title, "filters": fs, "defaultName": cfgStampRe.ReplaceAllString(filepath.Base(defaultPath), "STAMP")})
		if len(r.saveNext) == 0 {
			return "", nil
		}
		p := r.saveNext[0]
		r.saveNext = r.saveNext[1:]
		return p, nil
	}
	r.b = &Bridge{handlers: map[string]handler{}, sends: map[string]func([]json.RawMessage){}}
	r.l.registerShortcutsIPC(r.b)
	r.l.registerConfigTransfer(r.b)
	r.l.startShortcuts(true)
	return r
}

func (r *ctRun) normPath(s string) string {
	s = strings.ReplaceAll(s, r.ctDir, "<CT>")
	s = strings.ReplaceAll(s, r.cfgDir, "<CFG>")
	s = strings.ReplaceAll(s, r.userData, "<UD>")
	s = strings.ReplaceAll(s, "\\", "/")
	return cfgStampRe.ReplaceAllStringFunc(s, func(m string) string {
		if m >= "20250101-000000" {
			return "STAMP"
		}
		return m
	})
}

func (r *ctRun) normValue(v any) any {
	switch x := v.(type) {
	case string:
		return r.normPath(x)
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = r.normValue(e)
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			out[k] = r.normValue(e)
		}
		return out
	}
	return v
}

var jsonErrNorm = regexp.MustCompile(`^(读取配置文件失败：)(.*)$`)
var applyJSONErrNorm = regexp.MustCompile(`^导入失败：.*。导入前的配置已备份在`)

// JSON 语法错误的原文来自 V8 的 JSON.parse，Go 解析器的文案不同——属已接受差异，只比前缀与其余字段。
func normalizeJSONErrorText(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	out := map[string]any{}
	for k, e := range m {
		out[k] = normalizeJSONErrorText(e)
	}
	if s, ok := out["error"].(string); ok {
		if jsonErrNorm.MatchString(s) && !strings.Contains(s, "ENOENT") {
			out["error"] = "读取配置文件失败：<JSON>"
		}
		if applyJSONErrNorm.MatchString(s) {
			out["error"] = applyJSONErrNorm.ReplaceAllString(s, "导入失败：<JSON>。导入前的配置已备份在")
		}
	}
	return out
}

func stableExport(o map[string]any) map[string]any {
	if _, ok := o["exportedAt"]; ok {
		o["exportedAt"] = "NOW"
	}
	if _, ok := o["appVersion"]; ok {
		o["appVersion"] = "VER"
	}
	return o
}

func TestConfigTransferParity(t *testing.T) {
	g := loadCfgGolden(t)
	ct := g.ConfigTransfer
	for _, sc := range ct.Scenarios {
		t.Run(sc.Name, func(t *testing.T) {
			r := newCtRun(t, ct.Seeds[sc.Seed])
			lastPicked := ""
			for i, op := range sc.Ops {
				want := sc.Results[i]
				switch op.str("op") {
				case "export":
					var save map[string]any
					_ = json.Unmarshal(op["save"], &save)
					target := ""
					if p, ok := save["path"].(string); ok {
						target = filepath.Join(r.ctDir, p+".json")
						r.saveNext = append(r.saveNext, target)
					} else if save != nil {
						r.saveNext = append(r.saveNext, "")
					}
					r.saveCalls = nil
					got := callHandler(t, r.b, chConfigExport, opArgs(t, op, "request"))
					gotMap := map[string]any{"result": r.normValue(got)}
					if target != "" {
						if text, err := os.ReadFile(target); err == nil {
							fileText := string(text)
							parsed, perr := parseOrdered(text)
							if perr != nil {
								t.Fatalf("导出文件不是合法 JSON：%v", perr)
							}
							root := parsed.(*omap)
							if encAny, ok := root.vals["encryption"]; ok && op.notNull("password") {
								password := op.str("password")
								enc, msg := parseConfigEncryption(encAny, true)
								if enc == nil {
									t.Fatalf("导出的加密块无效：%s", msg)
								}
								cp, _ := parseConfigTransferFile(parsed)
								if cp == nil {
									t.Fatal("导出文件无法再次解析")
								}
								dec := decryptConfigSecrets(cp, password)
								gotMap["decryptOk"] = dec.ok
								gotMap["decryptFailed"] = float64(dec.failed)
								saltBytes := nodeBase64Decode(enc.Salt)
								if len(saltBytes) != 16 {
									t.Errorf("salt 长度 %d，应为 16", len(saltBytes))
								}
								gotMap["encryption"] = map[string]any{"algo": "aes-256-gcm", "kdf": "scrypt", "salt": "SALT", "n": float64(enc.N), "r": float64(enc.R), "p": float64(enc.P), "canary": "CANARY"}
								if cp.Settings != nil {
									root.set("settings", cp.Settings)
								}
								omapDelete(root, "encryption")
								plain := jsonRoundTrip(t, root).(map[string]any)
								gotMap["plain"] = stableExport(plain)
							} else {
								gotMap["plain"] = stableExport(jsonRoundTrip(t, root).(map[string]any))
								raw := regexp.MustCompile(`"exportedAt": "[^"]*"`).ReplaceAllString(fileText, `"exportedAt": "NOW"`)
								raw = regexp.MustCompile(`"appVersion": "[^"]*"`).ReplaceAllString(raw, `"appVersion": "VER"`)
								gotMap["raw"] = raw
							}
						}
					}
					calls := []any{}
					for _, c := range r.saveCalls {
						calls = append(calls, c)
					}
					gotMap["dialog"] = calls
					wantMap := want
					if _, ok := wantMap["dialog"]; !ok {
						delete(gotMap, "dialog")
					}
					assertCtResult(t, i, "export", wantMap, gotMap)
				case "read":
					switch {
					case op.str("pick") == "cancel":
						r.pickNext = append(r.pickNext, "")
					case op.str("pick") == "missing":
						lastPicked = filepath.Join(r.ctDir, "does-not-exist.json")
						r.pickNext = append(r.pickNext, lastPicked)
					default:
						lastPicked = filepath.Join(r.ctDir, "read-"+sc.Name+".json")
						if err := os.WriteFile(lastPicked, []byte(ct.Files[op.str("file")]), 0o644); err != nil {
							t.Fatal(err)
						}
						r.pickNext = append(r.pickNext, lastPicked)
					}
					got := callHandler(t, r.b, chConfigRead, nil)
					assertCtResult(t, i, "read", want, map[string]any{"result": r.normValue(got)})
				case "apply":
					filePath := lastPicked
					switch op.str("pathMode") {
					case "empty":
						filePath = ""
					case "other":
						filePath = filepath.Join(r.ctDir, "other.json")
					}
					args := []json.RawMessage{}
					pathRaw, _ := json.Marshal(filePath)
					args = append(args, pathRaw)
					hasSel, hasPw := op.has("selection"), op.has("password")
					if hasPw || hasSel {
						pwRaw := json.RawMessage("null")
						if hasPw {
							pwRaw = op["password"]
						}
						args = append(args, pwRaw)
					}
					if hasSel {
						args = append(args, op["selection"])
					}
					got := callHandler(t, r.b, chConfigApply, args)
					assertCtResult(t, i, "apply", want, map[string]any{"result": r.normValue(got)})
				}
			}
			equalEnv(t, sc.Name, sc.After, dumpEnvFiles(r.userData, r.cfgDir))
		})
	}
}

func assertCtResult(t *testing.T, i int, op string, want, got map[string]any) {
	t.Helper()
	w := normalizeJSONErrorText(want)
	gg := normalizeJSONErrorText(got)
	if reflect.DeepEqual(w, gg) {
		return
	}
	var diffs []string
	diffAny("", w, gg, &diffs)
	if len(diffs) > 12 {
		diffs = append(diffs[:12], "…")
	}
	t.Errorf("op %d %s 结果不一致：\n%s", i, op, strings.Join(diffs, "\n"))
}

func diffAny(path string, w, g any, out *[]string) {
	switch wv := w.(type) {
	case map[string]any:
		gv, ok := g.(map[string]any)
		if !ok {
			*out = append(*out, fmt.Sprintf("%s: TS=对象 Go=%.200v", path, g))
			return
		}
		keys := map[string]bool{}
		for k := range wv {
			keys[k] = true
		}
		for k := range gv {
			keys[k] = true
		}
		var ks []string
		for k := range keys {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		for _, k := range ks {
			a, aok := wv[k]
			b, bok := gv[k]
			switch {
			case aok && !bok:
				*out = append(*out, fmt.Sprintf("%s.%s: Go 缺失（TS=%.200v）", path, k, a))
			case !aok && bok:
				*out = append(*out, fmt.Sprintf("%s.%s: Go 多出（%.200v）", path, k, b))
			default:
				diffAny(path+"."+k, a, b, out)
			}
		}
	case []any:
		gv, ok := g.([]any)
		if !ok || len(gv) != len(wv) {
			*out = append(*out, fmt.Sprintf("%s: 数组长度/类型不同 TS=%.300v Go=%.300v", path, w, g))
			return
		}
		for i := range wv {
			diffAny(fmt.Sprintf("%s[%d]", path, i), wv[i], gv[i], out)
		}
	default:
		if !reflect.DeepEqual(w, g) {
			*out = append(*out, fmt.Sprintf("%s: TS=%.300v Go=%.300v", path, w, g))
		}
	}
}

// Go 加密的文件必须能被 TS 的解密读开：导出一份带密码的文件到 GUIZHI_DUMP_DIR，供 spikes/mygo-db/config-decrypt-check.mts 校验。
func TestConfigExportEncryptedDump(t *testing.T) {
	dump := os.Getenv("GUIZHI_DUMP_DIR")
	if dump == "" {
		t.Skip("未设置 GUIZHI_DUMP_DIR")
	}
	r := newCtRun(t, nil)
	target := filepath.Join(dump, "go-encrypted-config.json")
	r.saveNext = []string{target}
	req := `{"settings":{"theme":"dark","aiApiKey":"sk-秘密","networkProxy":{"password":"代理口令"},"aiProviders":[{"id":"p","apiKey":"k1"}],"aiModels":[{"id":"m","apiKey":"k2"}]},"includeSecrets":true,"password":"口令-pw"}`
	res := callHandler(t, r.b, chConfigExport, []json.RawMessage{json.RawMessage(req)})
	if m, _ := res.(map[string]any)["value"].(map[string]any); m == nil || m["success"] != true {
		t.Fatalf("导出失败：%v", res)
	}
	t.Logf("已写出 %s", target)
}
