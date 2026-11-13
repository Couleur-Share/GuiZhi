package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

// 本地备份逻辑与 TS（backup.ts）的差分：golden 由 spikes/mygo-db/golden-backup.mts 生成。

type backupGoldenFile struct {
	Backup struct {
		List []struct {
			FileName   string `json:"fileName"`
			Kind       string `json:"kind"`
			SizeBytes  int64  `json:"sizeBytes"`
			CreatedAt  int64  `json:"createdAt"`
			Format     string `json:"format"`
			Encrypted  bool   `json:"encrypted"`
			Validation string `json:"validation"`
		} `json:"list"`
		Del []struct {
			Name   string `json:"name"`
			Result struct {
				Success bool   `json:"success"`
				Error   string `json:"error"`
			} `json:"result"`
		} `json:"del"`
		Prune []struct {
			Kind      string   `json:"kind"`
			Keep      int      `json:"keep"`
			Removed   int      `json:"removed"`
			Remaining []string `json:"remaining"`
		} `json:"prune"`
		Validate []struct {
			Name   string `json:"name"`
			Result struct {
				OK    bool   `json:"ok"`
				Error string `json:"error"`
			} `json:"result"`
		} `json:"validate"`
		Swap []struct {
			Name               string    `json:"name"`
			Threw              bool      `json:"threw"`
			HasPreRestoreName  bool      `json:"hasPreRestoreName"`
			DB                 *string   `json:"db"`
			Sidecars           []bool    `json:"sidecars"`
			Backups            *[]string `json:"backups"`
			PreRestoreContents *[]string `json:"preRestoreContents"`
		} `json:"swap"`
	} `json:"backup"`
}

// 与 golden-backup.mts 的 listNames 保持一致（文件大小 = 序号 + 1）。
var backupGoldenListNames = []string{
	"knowledge-manual-20261101-120000.db",
	"knowledge-manual-20261101-120000-1.db",
	"knowledge-manual-20261101-120000-10.db",
	"knowledge-auto-20261101-130000.db",
	"knowledge-auto-20261102-130000.db",
	"knowledge-auto-20261031-130000.db",
	"knowledge-pre-update-20261031-235959.db",
	"knowledge-pre-restore-20250101-000000-12.db",
	"knowledge-manual-20261301-250000.db",
	"knowledge-weird-20261101-120000.db",
	"knowledge-manual-20261101-120000.db.bak",
	"notes.txt",
	"knowledge-auto-2026110-120000.db",
}

func seedBackupDir(t *testing.T, dir string) {
	t.Helper()
	os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i, n := range backupGoldenListNames {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(strings.Repeat("x", i+1)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func loadBackupGolden(t *testing.T) (backupGoldenFile, string) {
	t.Helper()
	dir := os.Getenv("GUIZHI_GOLDEN_DIR")
	if dir == "" {
		t.Skip("未设置 GUIZHI_GOLDEN_DIR")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g backupGoldenFile
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	if len(g.Backup.List) == 0 {
		t.Fatal("golden 缺少 backup 段")
	}
	return g, dir
}

func TestLocalBackupParity(t *testing.T) {
	g, goldenDir := loadBackupGolden(t)

	t.Run("list", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "list")
		seedBackupDir(t, dir)
		lb := &localBackups{backupsDir: dir}
		got := lb.list()
		if len(got) != len(g.Backup.List) {
			t.Fatalf("条数 %d，TS %d", len(got), len(g.Backup.List))
		}
		for i, w := range g.Backup.List {
			x := got[i]
			if x.FileName != w.FileName || x.Kind != w.Kind || x.SizeBytes != w.SizeBytes || x.CreatedAt != w.CreatedAt ||
				x.Format != w.Format || x.Encrypted != w.Encrypted || x.Validation != w.Validation {
				t.Errorf("第 %d 项不一致\n go: %+v\n ts: %+v", i, x, w)
			}
		}
	})

	t.Run("delete", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "del")
		lb := &localBackups{backupsDir: dir}
		for i, c := range g.Backup.Del {
			if i == 0 {
				seedBackupDir(t, dir)
			}
			r := lb.delete(c.Name)
			if r.Success != c.Result.Success || r.Error != c.Result.Error {
				t.Errorf("%q: go=%+v ts=%+v", c.Name, r, c.Result)
			}
		}
	})

	t.Run("prune", func(t *testing.T) {
		for _, c := range g.Backup.Prune {
			dir := filepath.Join(t.TempDir(), "p")
			seedBackupDir(t, dir)
			lb := &localBackups{backupsDir: dir}
			removed := lb.pruneOfKind(c.Kind, c.Keep)
			entries, _ := os.ReadDir(dir)
			var names []string
			for _, e := range entries {
				names = append(names, e.Name())
			}
			sort.Strings(names)
			if removed != c.Removed || !reflect.DeepEqual(names, c.Remaining) {
				t.Errorf("%s keep=%d: go removed=%d %v\n ts removed=%d %v", c.Kind, c.Keep, removed, names, c.Removed, c.Remaining)
			}
		}
	})

	t.Run("validate", func(t *testing.T) {
		vdir := filepath.Join(goldenDir, "backup-golden", "validate")
		for _, c := range g.Backup.Validate {
			ok, msg := validateBackupFile(filepath.Join(vdir, c.Name))
			if ok != c.Result.OK {
				t.Errorf("%s: ok go=%v ts=%v（go: %s / ts: %s）", c.Name, ok, c.Result.OK, msg, c.Result.Error)
				continue
			}
			// 驱动 / V8 的原生错误文本不同属已接受差异，仅比较前缀与固定中文文案
			if msg != c.Result.Error {
				const readPrefix = "备份文件无法读取: "
				if strings.HasPrefix(msg, readPrefix) && strings.HasPrefix(c.Result.Error, readPrefix) &&
					(c.Name == "garbage.db" || c.Name == "truncated.db" || c.Name == "snap-not-json.db") {
					continue
				}
				t.Errorf("%s: 错误文本 go=%q ts=%q", c.Name, msg, c.Result.Error)
			}
		}
	})

	t.Run("swap", func(t *testing.T) {
		stamp := regexp.MustCompile(`knowledge-pre-restore-\d{8}-\d{6}(-\d+)?\.db`)
		for _, c := range g.Backup.Swap {
			d := filepath.Join(t.TempDir(), c.Name)
			dbPath := filepath.Join(d, "data", "knowledge.db")
			backups := filepath.Join(d, "backups")
			src := filepath.Join(d, "incoming-source.db")
			os.MkdirAll(filepath.Dir(dbPath), 0o755)
			side := func() {
				for _, s := range []string{"-wal", "-shm", "-journal", ".clean"} {
					os.WriteFile(dbPath+s, []byte("side"), 0o644)
				}
			}
			switch c.Name {
			case "normal":
				os.WriteFile(dbPath, []byte("OLD"), 0o644)
				side()
				os.WriteFile(src, []byte("NEW"), 0o644)
			case "no-existing-db":
				os.WriteFile(src, []byte("NEW"), 0o644)
			case "missing-backup":
				os.WriteFile(dbPath, []byte("OLD"), 0o644)
				side()
			case "prune-old-pre-restore":
				os.WriteFile(dbPath, []byte("OLD"), 0o644)
				os.WriteFile(src, []byte("NEW"), 0o644)
				os.MkdirAll(backups, 0o755)
				for i, n := range []string{"20250101-000000", "20250102-000000", "20250103-000000", "20250104-000000"} {
					os.WriteFile(filepath.Join(backups, "knowledge-pre-restore-"+n+".db"), []byte("P"+string(rune('0'+i))), 0o644)
				}
			default:
				t.Fatalf("未知场景 %s", c.Name)
			}
			lb := &localBackups{dbPath: dbPath, backupsDir: backups}
			pre, err := lb.performRestoreSwap(src)
			if (err != nil) != c.Threw {
				t.Errorf("%s: threw go=%v ts=%v (%v)", c.Name, err != nil, c.Threw, err)
			}
			if (pre != "") != c.HasPreRestoreName {
				t.Errorf("%s: preRestore 名 go=%q ts=%v", c.Name, pre, c.HasPreRestoreName)
			}
			var gotDB *string
			if b, err := os.ReadFile(dbPath); err == nil {
				s := string(b)
				gotDB = &s
			}
			if !reflect.DeepEqual(gotDB, c.DB) {
				t.Errorf("%s: 主库内容 go=%v ts=%v", c.Name, gotDB, c.DB)
			}
			var sc []bool
			for _, s := range []string{"-wal", "-shm", "-journal", ".clean", ".incoming"} {
				_, err := os.Stat(dbPath + s)
				sc = append(sc, err == nil)
			}
			if !reflect.DeepEqual(sc, c.Sidecars) {
				t.Errorf("%s: 旁车文件 go=%v ts=%v", c.Name, sc, c.Sidecars)
			}
			var names, contents []string
			haveDir := false
			if entries, err := os.ReadDir(backups); err == nil {
				haveDir = true
				for _, e := range entries {
					names = append(names, stamp.ReplaceAllString(e.Name(), "knowledge-pre-restore-<TS>.db"))
					if strings.HasPrefix(e.Name(), "knowledge-pre-restore-") {
						b, _ := os.ReadFile(filepath.Join(backups, e.Name()))
						contents = append(contents, string(b))
					}
				}
				sort.Strings(names)
				sort.Strings(contents)
			}
			if haveDir != (c.Backups != nil) {
				t.Errorf("%s: 备份目录存在性 go=%v ts=%v", c.Name, haveDir, c.Backups != nil)
			} else if haveDir && (!reflect.DeepEqual(names, *c.Backups) || !reflect.DeepEqual(contents, *c.PreRestoreContents)) {
				t.Errorf("%s: 备份目录 go=%v %v\n ts=%v %v", c.Name, names, contents, *c.Backups, *c.PreRestoreContents)
			}
		}
	})
}

// ── 手写测试 ──

func TestBackupTimestampSequence(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "k.db")
	os.WriteFile(db, []byte("x"), 0o644)
	fixed := time.Date(2026, 11, 12, 8, 30, 5, 0, time.Local)
	lb := &localBackups{dbPath: db, backupsDir: dir, now: func() time.Time { return fixed }}
	a := lb.resolveBackupFileName("manual")
	os.WriteFile(filepath.Join(dir, a), nil, 0o644)
	b := lb.resolveBackupFileName("manual")
	os.WriteFile(filepath.Join(dir, b), nil, 0o644)
	c := lb.resolveBackupFileName("manual")
	if a != "knowledge-manual-20261112-083005.db" || b != "knowledge-manual-20261112-083005-1.db" || c != "knowledge-manual-20261112-083005-2.db" {
		t.Fatalf("同秒序号不符: %s %s %s", a, b, c)
	}
}

func TestRendererBackupSettingsKeepsKeyOrder(t *testing.T) {
	r := &rendererBackupSettings{}
	raw := json.RawMessage(`{"zeta":1,"alpha":{"b":2,"a":1},"mid":[3,2]}`)
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	r.setRaw(raw, m)
	o := r.ordered()
	if o == nil {
		t.Fatal("ordered 返回 nil")
	}
	out, err := marshalNoEscape(o)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != `{"zeta":1,"alpha":{"b":2,"a":1},"mid":[3,2]}` {
		t.Fatalf("键顺序丢失: %s", got)
	}
}
