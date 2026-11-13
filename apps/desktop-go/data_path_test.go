package main

// 数据目录差分测试：golden.dataPath 由真实 data-path.ts / shell-open-path.ts 在临时目录树里生成；
// 迁移/切换（applyDataPathChange 在 index.ts 内，无法直接差分）用手写场景覆盖。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/egoist/mygo/plugins/sqlite"
)

type dataPathGolden struct {
	DataPath struct {
		Inspect []struct {
			Name       string  `json:"name"`
			Tree       dpTree  `json:"tree"`
			Target     string  `json:"target"`
			Inspection dpInsp  `json:"inspection"`
			LinkSafe   bool    `json:"linkSafe"`
			Existing   bool    `json:"hasExisting"`
			_          *string `json:"-"`
		} `json:"inspect"`
		ReadCfg []struct {
			Name  string  `json:"name"`
			File  *string `json:"file"`
			Value *string `json:"value"`
		} `json:"readCfg"`
		WriteResult string `json:"writeResult"`
		Resolve     []struct {
			Name     string `json:"name"`
			Tree     dpTree `json:"tree"`
			Exe      string `json:"exe"`
			Packaged bool   `json:"packaged"`
			Platform string `json:"platform"`
			Value    string `json:"value"`
		} `json:"resolve"`
		Open []struct {
			Name   string           `json:"name"`
			Tree   dpTree           `json:"tree"`
			Arg    any              `json:"arg"`
			Result map[string]any   `json:"result"`
			Calls  []map[string]any `json:"calls"`
		} `json:"open"`
	} `json:"dataPath"`
}

type dpTree map[string]*string

type dpInsp struct {
	TargetPath      string       `json:"targetPath"`
	Exists          bool         `json:"exists"`
	HasExistingData bool         `json:"hasExistingData"`
	Markers         []dataMarker `json:"markers"`
}

const dpRoot = "<ROOT>"

func dpBuild(t *testing.T, root string, tree dpTree) {
	t.Helper()
	os.RemoveAll(root)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, content := range tree {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if content == nil {
			if err := os.MkdirAll(full, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(*content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func dpNorm(s, root string) string {
	s = strings.ReplaceAll(s, root, dpRoot)
	s = strings.ReplaceAll(s, filepath.ToSlash(root), dpRoot)
	return strings.ReplaceAll(s, "\\", "/")
}

func TestDataPathParity(t *testing.T) {
	dir := os.Getenv("GUIZHI_GOLDEN_DIR")
	if dir == "" {
		t.Skip("未设置 GUIZHI_GOLDEN_DIR")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g dataPathGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	dp := g.DataPath

	t.Run("inspect", func(t *testing.T) {
		if len(dp.Inspect) == 0 {
			t.Fatal("golden 缺少 inspect 场景")
		}
		for _, c := range dp.Inspect {
			t.Run(c.Name, func(t *testing.T) {
				root := filepath.Join(t.TempDir(), "r")
				dpBuild(t, root, c.Tree)
				target := ""
				if c.Target != "<empty>" {
					target = filepath.Join(root, filepath.FromSlash(c.Target))
					if strings.HasSuffix(c.Target, "/") {
						target += string(filepath.Separator)
					}
				}
				got, err := inspectDataPath(target)
				if err != nil {
					t.Fatal(err)
				}
				if c.Target != "<empty>" {
					got.TargetPath = dpNorm(got.TargetPath, root)
					for i := range got.Markers {
						got.Markers[i].Path = dpNorm(got.Markers[i].Path, root)
					}
					if got.Markers == nil {
						got.Markers = []dataMarker{}
					}
					want := c.Inspection
					if want.Markers == nil {
						want.Markers = []dataMarker{}
					}
					if !reflect.DeepEqual(got, dataPathInspection(want)) {
						t.Fatalf("inspect 不一致\n got=%+v\nwant=%+v", got, want)
					}
				} else if got.Exists != c.Inspection.Exists || got.HasExistingData != c.Inspection.HasExistingData {
					t.Fatalf("空串 inspect 不一致 got=%+v want=%+v", got, c.Inspection)
				}
				lsTarget := target
				if lsTarget == "" {
					lsTarget = root
				}
				if safe, _ := isLinkSafeDataPathRoot(lsTarget); safe != c.LinkSafe {
					t.Fatalf("linkSafe got=%v want=%v", safe, c.LinkSafe)
				}
				if has := hasExistingAppData(target); has != c.Existing {
					t.Fatalf("hasExisting got=%v want=%v", has, c.Existing)
				}
			})
		}
	})

	t.Run("readConfigured", func(t *testing.T) {
		for _, c := range dp.ReadCfg {
			t.Run(c.Name, func(t *testing.T) {
				root := filepath.Join(t.TempDir(), "r")
				os.MkdirAll(root, 0o755)
				if c.File != nil {
					fwd := filepath.ToSlash(root)
					text := strings.ReplaceAll(strings.ReplaceAll(*c.File, dpRoot, fwd), "{ROOTFWD}", fwd)
					os.MkdirAll(filepath.Join(root, "GuiZhi"), 0o755)
					os.WriteFile(filepath.Join(root, "GuiZhi", "data-path.json"), []byte(text), 0o644)
				}
				got := readConfiguredDataPath(root)
				want := ""
				if c.Value != nil {
					want = *c.Value
				}
				if dpNorm(got, root) != want && !(got == "" && want == "") {
					t.Fatalf("got=%q want=%q", dpNorm(got, root), want)
				}
			})
		}
	})

	t.Run("writeConfigured", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "w")
		os.MkdirAll(root, 0o755)
		if err := writeConfiguredDataPath(root, filepath.Join(root, "x", "..", "dest")); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(root, "GuiZhi", "data-path.json"))
		if err != nil {
			t.Fatal(err)
		}
		text := dpUpdatedAtRe.ReplaceAllString(string(b), `"updatedAt": "NOW"`)
		text = strings.ReplaceAll(text, strings.ReplaceAll(root, `\`, `\\`), dpRoot)
		text = strings.ReplaceAll(text, `\\`, `/`)
		want := dp.WriteResult
		if text != want {
			t.Fatalf("写出内容不一致\n got=%s\nwant=%s", text, want)
		}
	})

	t.Run("resolveInitial", func(t *testing.T) {
		for _, c := range dp.Resolve {
			t.Run(c.Name, func(t *testing.T) {
				root := filepath.Join(t.TempDir(), "r")
				fwd := filepath.ToSlash(root)
				tree := dpTree{}
				for k, v := range c.Tree {
					if v != nil {
						s := strings.ReplaceAll(*v, dpRoot, fwd)
						v = &s
					}
					tree[k] = v
				}
				dpBuild(t, root, tree)
				appData := filepath.Join(root, "appdata")
				os.MkdirAll(appData, 0o755)
				got := resolveInitialUserDataPath(dataPathOptions{
					AppDataPath:         appData,
					DefaultUserDataPath: historicalDefaultUserDataPath(appData),
					ExePath:             strings.ReplaceAll(c.Exe, dpRoot, fwd),
					IsPackaged:          c.Packaged,
					Platform:            c.Platform,
				}, defaultDataPathDeps)
				if dpNorm(got, root) != c.Value {
					t.Fatalf("got=%q want=%q", dpNorm(got, root), c.Value)
				}
			})
		}
	})

	t.Run("shellOpenPath", func(t *testing.T) {
		for _, c := range dp.Open {
			t.Run(c.Name, func(t *testing.T) {
				root := filepath.Join(t.TempDir(), "r")
				dpBuild(t, root, c.Tree)
				arg, _ := c.Arg.(string)
				arg = strings.ReplaceAll(arg, dpRoot, filepath.ToSlash(root))
				calls := []map[string]any{}
				res := openDirectoryPath(arg, openPathDeps{
					appDataPath: filepath.Join(root, "appdata"), homePath: filepath.Join(root, "home"), localAppDataPath: filepath.Join(root, "local"),
					lstat: os.Lstat, stat: os.Stat,
					openPath: func(p string) error {
						calls = append(calls, map[string]any{"openPath": dpNorm(p, root)})
						if c.Name == "openPathError" {
							return errString("boom: cannot open")
						}
						return nil
					},
					showItemInFolder: func(p string) { calls = append(calls, map[string]any{"showItemInFolder": dpNorm(p, root)}) },
				})
				if !reflect.DeepEqual(res, c.Result) {
					t.Fatalf("result got=%v want=%v", res, c.Result)
				}
				want := c.Calls
				if want == nil {
					want = []map[string]any{}
				}
				if !reflect.DeepEqual(calls, want) {
					t.Fatalf("calls got=%v want=%v", calls, want)
				}
			})
		}
	})
}

var dpUpdatedAtRe = regexp.MustCompile(`"updatedAt": "[^"]*"`)

type errString string

func (e errString) Error() string { return string(e) }

// ── 手写场景：迁移/切换/预览 ──

func newDataPathLib(t *testing.T) (*Library, string, string) {
	t.Helper()
	base := t.TempDir()
	cur := filepath.Join(base, "cur")
	appData := filepath.Join(base, "appdata")
	for _, d := range []string{cur, appData, filepath.Join(cur, "data"), filepath.Join(cur, "config")} {
		os.MkdirAll(d, 0o755)
	}
	os.WriteFile(filepath.Join(cur, "data", "knowledge.db"), []byte("DB"), 0o644)
	os.WriteFile(filepath.Join(cur, "config", "ai.json"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(cur, "shortcuts.json"), []byte("{}"), 0o644)
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(base, "t.db"), sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Execute(ctx, "CREATE TABLE import_tasks (id TEXT, status TEXT)"); err != nil {
		t.Fatal(err)
	}
	return &Library{db: db, appData: appData, paths: Paths{UserData: cur}}, cur, base
}

func TestApplyDataPathChange(t *testing.T) {
	ctx := context.Background()
	str := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return *p
	}

	t.Run("参数校验", func(t *testing.T) {
		l, _, base := newDataPathLib(t)
		if r := l.applyDataPathChange(ctx, "  ", "migrate"); r.Success || str(r.Error) != "data path change requires a non-empty newPath string" {
			t.Fatalf("空路径: %+v", r)
		}
		if r := l.applyDataPathChange(ctx, filepath.Join(base, "x"), "bogus"); r.Success || str(r.Error) != "Unsupported data path change action: bogus" {
			t.Fatalf("非法 action: %+v", r)
		}
	})

	t.Run("目标即当前目录", func(t *testing.T) {
		l, cur, _ := newDataPathLib(t)
		r := l.applyDataPathChange(ctx, cur+string(filepath.Separator), "migrate")
		if !r.Success || str(r.Message) != "Data directory is already current" || r.NeedsRestart == nil || *r.NeedsRestart {
			t.Fatalf("%+v", r)
		}
	})

	t.Run("migrate 拷贝并写配置", func(t *testing.T) {
		l, _, base := newDataPathLib(t)
		dest := filepath.Join(base, "dest")
		r := l.applyDataPathChange(ctx, dest, "migrate")
		if !r.Success || str(r.Message) != "Successfully migrated 3 items" || r.NeedsRestart == nil || !*r.NeedsRestart {
			t.Fatalf("%+v", r)
		}
		if b, _ := os.ReadFile(filepath.Join(dest, "data", "knowledge.db")); string(b) != "DB" {
			t.Fatalf("数据库没拷贝过去: %q", b)
		}
		if got := readConfiguredDataPath(l.appData); got != nodeResolve(dest) {
			t.Fatalf("配置未写入: %q", got)
		}
	})

	t.Run("migrate 不覆盖已有文件，overwrite 覆盖", func(t *testing.T) {
		l, _, base := newDataPathLib(t)
		dest := filepath.Join(base, "dest")
		os.MkdirAll(filepath.Join(dest, "config"), 0o755)
		os.WriteFile(filepath.Join(dest, "config", "ai.json"), []byte("OLD"), 0o644)
		if r := l.applyDataPathChange(ctx, dest, "migrate"); !r.Success {
			t.Fatalf("%+v", r)
		}
		if b, _ := os.ReadFile(filepath.Join(dest, "config", "ai.json")); string(b) != "OLD" {
			t.Fatalf("migrate 不应覆盖: %q", b)
		}
		if r := l.applyDataPathChange(ctx, dest, "overwrite"); !r.Success {
			t.Fatalf("%+v", r)
		}
		if b, _ := os.ReadFile(filepath.Join(dest, "config", "ai.json")); string(b) != "{}" {
			t.Fatalf("overwrite 应覆盖: %q", b)
		}
	})

	t.Run("switch 要求目标存在，且不拷贝", func(t *testing.T) {
		l, _, base := newDataPathLib(t)
		dest := filepath.Join(base, "dest")
		if r := l.applyDataPathChange(ctx, dest, "switch"); r.Success || !strings.HasPrefix(str(r.Error), "Cannot switch to a directory that does not exist: ") {
			t.Fatalf("%+v", r)
		}
		os.MkdirAll(dest, 0o755)
		r := l.applyDataPathChange(ctx, dest, "switch")
		if !r.Success || str(r.Message) != "Data directory switched" {
			t.Fatalf("%+v", r)
		}
		if _, err := os.Stat(filepath.Join(dest, "data")); err == nil {
			t.Fatal("switch 不应拷贝数据")
		}
	})

	t.Run("拒绝子目录迁移，允许 switch 到子目录", func(t *testing.T) {
		l, cur, _ := newDataPathLib(t)
		child := filepath.Join(cur, "sub")
		if r := l.applyDataPathChange(ctx, child, "migrate"); r.Success || str(r.Error) != "Cannot migrate data into a child directory of the current data directory" {
			t.Fatalf("%+v", r)
		}
		os.MkdirAll(child, 0o755)
		if r := l.applyDataPathChange(ctx, child, "switch"); !r.Success {
			t.Fatalf("%+v", r)
		}
	})

	t.Run("采集任务进行中拒绝", func(t *testing.T) {
		l, _, base := newDataPathLib(t)
		if _, err := l.db.Execute(ctx, "INSERT INTO import_tasks VALUES ('a','processing')"); err != nil {
			t.Fatal(err)
		}
		r := l.applyDataPathChange(ctx, filepath.Join(base, "dest"), "migrate")
		if r.Success || str(r.Error) != "有采集任务正在写入，请先暂停或取消任务再切换数据目录" {
			t.Fatalf("%+v", r)
		}
		if _, err := os.Stat(filepath.Join(base, "dest")); err == nil {
			t.Fatal("拒绝后不应创建目标目录")
		}
	})

	t.Run("主题排版进行中拒绝", func(t *testing.T) {
		l, _, base := newDataPathLib(t)
		l.hasActiveThemedReading = func() bool { return true }
		r := l.applyDataPathChange(ctx, filepath.Join(base, "dest"), "migrate")
		if r.Success || str(r.Error) != "主题排版正在执行，请先停止任务再切换数据目录" {
			t.Fatalf("%+v", r)
		}
	})

	t.Run("拒绝系统目录", func(t *testing.T) {
		l, _, _ := newDataPathLib(t)
		r := l.applyDataPathChange(ctx, `C:\Windows\GuiZhiData`, "migrate")
		if r.Success || !strings.HasPrefix(str(r.Error), "Cannot use system directory as data directory: ") {
			t.Fatalf("%+v", r)
		}
	})
}

func TestDataGetStatusAndPreview(t *testing.T) {
	l, cur, base := newDataPathLib(t)
	b := &Bridge{handlers: map[string]handler{}}
	l.registerDataPath(b)
	ctx := context.Background()
	call := func(ch string, args ...any) map[string]any {
		var raws []json.RawMessage
		for _, a := range args {
			j, _ := json.Marshal(a)
			raws = append(raws, j)
		}
		v, err := b.handlers[ch](ctx, raws)
		if err != nil {
			t.Fatalf("%s: %v", ch, err)
		}
		j, _ := json.Marshal(v)
		var m map[string]any
		json.Unmarshal(j, &m)
		return m
	}

	st := call("data:getStatus")
	if st["configuredPath"] != nil || st["currentPath"] != cur || st["needsRestart"] != false {
		t.Fatalf("初始状态: %v", st)
	}
	dest := filepath.Join(base, "dest")
	l.applyDataPathChange(ctx, dest, "migrate")
	st = call("data:getStatus")
	if st["configuredPath"] != nodeResolve(dest) || st["needsRestart"] != true {
		t.Fatalf("迁移后状态: %v", st)
	}

	if p := call("data:previewDataPathChange", ""); p["success"] != false {
		t.Fatalf("空路径预览: %v", p)
	}
	p := call("data:previewDataPathChange", dest)
	if p["success"] != true || p["exists"] != true || p["isCurrentPath"] != false || p["targetPath"] != nodeResolve(dest) {
		t.Fatalf("预览: %v", p)
	}
	if ms, _ := p["markers"].([]any); len(ms) != 4 { // data、config、shortcuts.json、data/knowledge.db
		t.Fatalf("markers: %v", p["markers"])
	}
	if p := call("data:previewDataPathChange", cur); p["isCurrentPath"] != true {
		t.Fatalf("当前目录: %v", p)
	}
	if p := call("data:previewDataPathChange", filepath.Join(base, "none")); p["exists"] != false {
		t.Fatalf("不存在目录: %v", p)
	}
}
