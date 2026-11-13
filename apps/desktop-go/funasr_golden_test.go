package main

// FunASR 本地转写引擎的差分测试：基线由真实 TS 实现产出（spikes/mygo-db/golden-funasr.mts）。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

type funasrGolden struct {
	RootForPaths string            `json:"rootForPaths"`
	Paths        map[string]string `json:"paths"`
	Versions     []struct {
		Latest  string `json:"latest"`
		Current string `json:"current"`
		Newer   bool   `json:"newer"`
		Error   string `json:"error"`
	} `json:"versions"`
	Logs []struct {
		Raw     string `json:"raw"`
		Summary string `json:"summary"`
		Short   string `json:"short"`
	} `json:"logs"`
	Proxy []struct {
		M   string `json:"m"`
		Out string `json:"out"`
	} `json:"proxy"`
	IDDir    string            `json:"idDir"`
	Identity directoryIdentity `json:"identity"`
	TreeDir  string            `json:"treeDir"`
	Measure  map[string]int64  `json:"measure"`
	Formats  []struct {
		N float64 `json:"n"`
		S string  `json:"s"`
	} `json:"formats"`
	Backup struct {
		Root           string            `json:"root"`
		RootIdentity   directoryIdentity `json:"rootIdentity"`
		State          funasrInstallState
		Manifests      map[string]string `json:"manifests"`
		Dirs           map[string]string `json:"dirs"`
		Labels         map[string]string `json:"labels"`
		Warnings       []string          `json:"warnings"`
		Remaining      []string          `json:"remaining"`
		DisposableRoot string            `json:"disposableRoot"`
		DisposableDir  string            `json:"disposableDir"`
	} `json:"backup"`
	AICases []struct {
		Name     string `json:"name"`
		Op       string `json:"op"`
		SeedText string `json:"seedText"`
		After    string `json:"after"`
	} `json:"aiCases"`
}

func loadFunasrGolden(t *testing.T) funasrGolden {
	t.Helper()
	dir := os.Getenv("GUIZHI_GOLDEN_DIR")
	if dir == "" {
		t.Skip("未设置 GUIZHI_GOLDEN_DIR")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Funasr *funasrGolden `json:"funasr"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	if g.Funasr == nil {
		t.Skip("golden 里没有 funasr 段，请重新生成")
	}
	return *g.Funasr
}

func TestFunasrGoldenPaths(t *testing.T) {
	g := loadFunasrGolden(t)
	p := funasrPathsFor(g.RootForPaths)
	got := map[string]string{
		"root": p.Root, "pythonExe": p.PythonExe, "venvDir": p.VenvDir, "venvPython": p.VenvPython,
		"serverScript": p.ServerScript, "funasrPackage": p.FunasrPackage, "modelsDir": p.ModelsDir,
		"ggufRuntimeDir": p.GgufRuntime, "ggufModelsDir": p.GgufModels, "sensevoiceGguf": p.SensevoiceGGU,
		"vadGguf": p.VadGguf, "sensevoiceCli": p.SensevoiceCli, "stateFile": p.StateFile,
	}
	for k, want := range g.Paths {
		if got[k] != want {
			t.Errorf("paths.%s = %q，TS 为 %q", k, got[k], want)
		}
	}
	if len(got) != len(g.Paths) {
		t.Errorf("字段数不一致：Go %d，TS %d", len(got), len(g.Paths))
	}
}

func TestFunasrGoldenVersions(t *testing.T) {
	g := loadFunasrGolden(t)
	for _, c := range g.Versions {
		newer, err := isNewerFunasrVersion(c.Latest, c.Current)
		if c.Error != "" {
			if err == nil || err.Error() != c.Error {
				t.Errorf("%q vs %q：期望错误 %q，得到 %v", c.Latest, c.Current, c.Error, err)
			}
			continue
		}
		if err != nil || newer != c.Newer {
			t.Errorf("%q vs %q：期望 %v，得到 %v / %v", c.Latest, c.Current, c.Newer, newer, err)
		}
	}
}

func TestFunasrGoldenLogsAndProxy(t *testing.T) {
	g := loadFunasrGolden(t)
	for i, c := range g.Logs {
		if got := summarizeProcessLog(c.Raw, 300); got != c.Summary {
			t.Errorf("日志 #%d 摘要：Go %q，TS %q", i, got, c.Summary)
		}
		if got := summarizeProcessLog(c.Raw, 20); got != c.Short {
			t.Errorf("日志 #%d 短摘要：Go %q，TS %q", i, got, c.Short)
		}
	}
	for _, c := range g.Proxy {
		if got := explainFunasrProxyFailure(c.M); got != c.Out {
			t.Errorf("代理改写 %q：Go %q，TS %q", c.M, got, c.Out)
		}
	}
}

func TestFunasrGoldenFilesystem(t *testing.T) {
	g := loadFunasrGolden(t)
	id, err := readDirectoryIdentity(g.IDDir)
	if err != nil || id != g.Identity {
		t.Errorf("目录身份：Go %+v (%v)，TS %+v", id, err, g.Identity)
	}
	for name, bs := range map[string]int64{"b512": 512, "b4096": 4096, "b65536": 65536} {
		got, err := measureFunasrEnvironment(g.TreeDir, bs)
		if err != nil || got != g.Measure[name] {
			t.Errorf("体积估算 %s：Go %d (%v)，TS %d", name, got, err, g.Measure[name])
		}
	}
	for _, f := range g.Formats {
		if got := formatFunasrBytes(int64(f.N)); got != f.S {
			t.Errorf("formatFunasrBytes(%v) = %q，TS %q", f.N, got, f.S)
		}
	}
}

var (
	reMaskDir      = regexp.MustCompile(`"directory": "[^"]*"`)
	reMaskPid      = regexp.MustCompile(`"ownerPid": \d+`)
	reMaskTime     = regexp.MustCompile(`"createdAt": "[^"]*"`)
	reMaskIdentity = regexp.MustCompile(`"(device|inode|birthtime)": "\d+"`)
)

func maskManifest(s string) string {
	s = reMaskDir.ReplaceAllString(s, `"directory": "D"`)
	s = reMaskPid.ReplaceAllString(s, `"ownerPid": 1`)
	s = reMaskTime.ReplaceAllString(s, `"createdAt": "T"`)
	return reMaskIdentity.ReplaceAllString(s, `"$1": "0"`)
}

// 备份清单：Go 写出的文本与 TS 一致（逐字，动态值除外），并且能读懂 TS 写下的清单与身份。
func TestFunasrGoldenBackupManifest(t *testing.T) {
	g := loadFunasrGolden(t)
	root := t.TempDir()
	b, err := createFunasrBackup(root, g.Backup.State, "1.3.30")
	if err != nil {
		t.Fatal(err)
	}
	if err := markFunasrBackup(b, backupReady); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(b.Directory, funasrBackupManifestName))
	if got, want := maskManifest(string(raw)), maskManifest(g.Backup.Manifests["ready"]); got != want {
		t.Errorf("清单文本不一致\nGo:\n%s\nTS:\n%s", got, want)
	}
	if !reBackupName.MatchString(filepath.Base(b.Directory)) {
		t.Errorf("备份目录名不符合约定：%s", b.Directory)
	}
	// TS 写下的清单：能解析，且其中的身份与现存目录一致
	rootID, err := readDirectoryIdentity(g.Backup.Root)
	if err != nil || rootID != g.Backup.RootIdentity {
		t.Errorf("TS 备份根目录身份：Go %+v (%v)，清单 %+v", rootID, err, g.Backup.RootIdentity)
	}
	for phase, name := range g.Backup.Dirs {
		dir := filepath.Join(g.Backup.Root, name)
		if _, err := os.Stat(dir); err != nil {
			continue // disposable 已被 TS 清理
		}
		m, err := readBackupManifest(g.Backup.Root, dir, name)
		if err != nil {
			t.Errorf("%s：无法读取 TS 清单：%v", phase, err)
			continue
		}
		if string(m.Phase) != phase {
			t.Errorf("%s：阶段读成 %q", phase, m.Phase)
		}
		if id, err := readDirectoryIdentity(dir); err != nil || id != m.DirectoryIdentity {
			t.Errorf("%s：备份目录身份 Go %+v (%v)，清单 %+v", phase, id, err, m.DirectoryIdentity)
		}
	}
}

func labelWarnings(ws []string, root string, labels map[string]string) []string {
	norm := func(s string) string { return strings.ReplaceAll(s, `\`, "/") }
	out := make([]string, len(ws))
	for i, w := range ws {
		w = strings.ReplaceAll(norm(w), norm(root), "<ROOT>")
		for name, label := range labels {
			w = strings.ReplaceAll(w, name, "<"+label+">")
		}
		out[i] = w
	}
	sort.Strings(out)
	return out
}

// 对 TS 留下的同一棵目录树跑 Go 的清理：判定与警告文字必须与 TS 对同一棵树的结论一致。
func TestFunasrGoldenBackupCleanup(t *testing.T) {
	g := loadFunasrGolden(t)
	if _, err := os.Stat(g.Backup.Root); err != nil {
		t.Skip("TS 备份目录树不存在，请重新生成 golden")
	}
	warnings, err := cleanupFunasrBackups(g.Backup.Root)
	if err != nil {
		t.Fatal(err)
	}
	got := labelWarnings(warnings, g.Backup.Root, g.Backup.Labels)
	want := append([]string(nil), g.Backup.Warnings...)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("清理警告不一致\nGo:\n%s\nTS:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	entries, _ := os.ReadDir(g.Backup.Root)
	var remaining []string
	for _, e := range entries {
		if label, ok := g.Backup.Labels[e.Name()]; ok {
			remaining = append(remaining, label)
		} else {
			remaining = append(remaining, e.Name())
		}
	}
	sort.Strings(remaining)
	if strings.Join(remaining, ",") != strings.Join(g.Backup.Remaining, ",") {
		t.Errorf("清理后剩余：Go %v，TS %v", remaining, g.Backup.Remaining)
	}
}

// 旧版（TS）留下的 disposable 备份，Go 要能认出并清掉。该目录只能原地清理，所以每份 golden 只能验证一次。
func TestFunasrGoldenConsumesTsDisposable(t *testing.T) {
	g := loadFunasrGolden(t)
	dir := filepath.Join(g.Backup.DisposableRoot, g.Backup.DisposableDir)
	if _, err := os.Stat(dir); err != nil {
		t.Skip("TS 的 disposable 备份已在先前的运行中被清掉；重新生成 golden 后可再次验证")
	}
	warnings, err := cleanupFunasrBackups(g.Backup.DisposableRoot)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("清理 TS 留下的 disposable：%v / %v", warnings, err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("备份目录应已被删除：%v", err)
	}
}

var reMaskStamps = regexp.MustCompile(`"(updatedAt|lastVerifiedAt)": "[^"]*"`)

// 内置转写模型写入 / 移除：落盘的 ai-models.json 与 TS 逐字一致（时间戳除外）。
func TestFunasrGoldenBuiltinTranscription(t *testing.T) {
	g := loadFunasrGolden(t)
	for _, c := range g.AICases {
		t.Run(c.Name, func(t *testing.T) {
			dir := t.TempDir()
			if c.SeedText != "" {
				if err := os.WriteFile(filepath.Join(dir, aiConfigFileName), []byte(c.SeedText), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			l := &Library{configDir: dir}
			if err := l.writeBuiltinTranscription(c.Op == "remove"); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(filepath.Join(dir, aiConfigFileName))
			if err != nil {
				t.Fatal(err)
			}
			got := reMaskStamps.ReplaceAllString(string(raw), `"$1": "NOW"`)
			if got != c.After {
				t.Errorf("文件不一致\nGo:\n%s\nTS:\n%s", got, c.After)
			}
		})
	}
}
