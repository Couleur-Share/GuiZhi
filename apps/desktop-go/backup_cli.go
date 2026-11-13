package main

// 备份仓库命令行（`-backupcli <子命令> …`）：用 Go 实现读/写/恢复同一个仓库，供 spikes/mygo-restore/run-diff.ps1
// 与 TS 真实实现互读互写、对拍恢复结果。子命令（userData 指模拟的用户数据目录，其下 backups/repository 是仓库）：
//
//	read     <srcRoot> <snapshotId> <password> <out.json>
//	preview  <srcRoot> <snapshotId> <password> <out.json>
//	restore  <srcRoot> <snapshotId> <password> <targetDir> <currentRendererSettings.json>
//	snapshot <srcRoot> <password> <rendererSettings.json|-> <kind> <out.json>
//	init     <srcRoot> <password>
//	changepw <srcRoot> <old> <new>
//	delete   <srcRoot> <snapshotId> <password>
//	portable <srcRoot> <snapshotId> <password> <out.zip>
//
// 环境变量 GUIZHI_SCHEMA_VERSION：当前应用支持的数据库结构版本（默认取 supportedSchemaVersion）。

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func bcliPathsFor(userData string) repoPaths {
	return repoPaths{
		RepositoryDir: filepath.Join(userData, "backups", "repository"),
		DatabasePath:  filepath.Join(userData, "data", "knowledge.db"),
		ConfigDir:     filepath.Join(userData, "config"),
		ImagesDir:     filepath.Join(userData, "data", "assets", "images"),
		VideosDir:     filepath.Join(userData, "data", "assets", "videos"),
	}
}

func bcliDie(err error) {
	fmt.Fprintln(os.Stderr, "错误：", err)
	os.Exit(1)
}

func bcliWriteJSON(path string, v any) {
	raw, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		bcliDie(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		bcliDie(err)
	}
}

func bcliReadOmap(path string) *omap {
	if path == "" || path == "-" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		bcliDie(err)
	}
	v, err := parseOrdered(raw)
	if err != nil {
		bcliDie(err)
	}
	m, _ := v.(*omap)
	return m
}

func runBackupCLI(args []string) int {
	if len(args) < 1 {
		bcliDie(fmt.Errorf("缺少子命令"))
	}
	schema, _ := strconv.Atoi(os.Getenv("GUIZHI_SCHEMA_VERSION"))
	if schema == 0 {
		schema = supportedSchemaVersion
	}
	cmd, a := args[0], args[1:]
	need := func(n int) {
		if len(a) < n {
			bcliDie(fmt.Errorf("%s 需要 %d 个参数", cmd, n))
		}
	}
	userData := func(root string) string { return filepath.Join(root, "src") }
	switch cmd {
	case "read":
		need(4)
		repo := &backupRepository{paths: bcliPathsFor(userData(a[0])), schemaVersion: schema}
		m, err := repo.readManifest(a[1], a[2])
		if err != nil {
			bcliDie(err)
		}
		type entryOut struct {
			snapshotEntry
			PlainSha string `json:"plainSha"`
		}
		entries := []entryOut{}
		for _, e := range m.Entries {
			plain, err := repo.readEntry(e)
			if err != nil {
				bcliDie(err)
			}
			entries = append(entries, entryOut{e, sha256Hex(plain)})
		}
		list := repo.listSnapshots()
		for i := range list {
			list[i].Path = ""
		}
		bcliWriteJSON(a[3], map[string]any{"manifest": map[string]any{
			"kind": m.Kind, "version": m.Version, "id": m.ID, "createdAt": m.CreatedAt, "appVersion": m.AppVersion,
			"schemaVersion": m.SchemaVersion, "entries": entries, "summary": m.Summary,
		}, "list": list})
	case "preview":
		need(4)
		repo := &backupRepository{paths: bcliPathsFor(userData(a[0])), schemaVersion: schema}
		p := previewRepositoryRestore(repo, a[1], a[2])
		if p.Snapshot != nil {
			p.Snapshot.Path = ""
		}
		bcliWriteJSON(a[3], p)
	case "restore":
		need(5)
		repo := &backupRepository{paths: bcliPathsFor(userData(a[0])), schemaVersion: schema}
		tp := bcliPathsFor(a[3])
		targets := restoreTargets{DatabasePath: tp.DatabasePath, ImagesDir: tp.ImagesDir, VideosDir: tp.VideosDir, ConfigDir: tp.ConfigDir}
		live := func(key string) (string, bool, error) {
			db, err := openReadOnly(targets.DatabasePath)
			if err != nil {
				return "", false, err
			}
			defer db.Close()
			var v string
			err = db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&v)
			if err == sql.ErrNoRows {
				return "", false, nil
			}
			return v, err == nil, err
		}
		if v := os.Getenv("GUIZHI_FAIL_AFTER"); v != "" { // 故障注入：第 n 次替换后失败，验证回滚
			n, _ := strconv.Atoi(v)
			applyHook = func(replaced int) error {
				if replaced == n {
					return fmt.Errorf("注入的失败（第 %d 次替换后）", n)
				}
				return nil
			}
		}
		prepared, err := prepareRepositoryRestore(repo, a[1], a[2], live, targets, bcliReadOmap(a[4]))
		if err != nil {
			bcliDie(err)
		}
		if err := applyPreparedRepositoryRestore(prepared, targets); err != nil {
			bcliDie(err)
		}
		fmt.Println("Go 恢复完成")
	case "snapshot":
		need(5)
		repo := &backupRepository{paths: bcliPathsFor(userData(a[0])), schemaVersion: schema}
		res := repo.createSnapshot(snapshotRequest{RecoveryPassword: a[1], RendererSettings: bcliReadOmap(a[2]), Kind: a[3], AppVersion: "0.28.0"})
		if res.Snapshot != nil {
			res.Snapshot.Path = ""
		}
		bcliWriteJSON(a[4], res)
		if !res.Success {
			bcliDie(fmt.Errorf("%s", res.Error))
		}
	case "init":
		need(2)
		repo := &backupRepository{paths: bcliPathsFor(userData(a[0])), schemaVersion: schema}
		if err := repo.initialize(a[1]); err != nil {
			bcliDie(err)
		}
	case "changepw":
		need(3)
		repo := &backupRepository{paths: bcliPathsFor(userData(a[0])), schemaVersion: schema}
		if err := repo.changePassword(a[1], a[2]); err != nil {
			bcliDie(err)
		}
	case "delete":
		need(3)
		repo := &backupRepository{paths: bcliPathsFor(userData(a[0])), schemaVersion: schema}
		n, err := repo.deleteSnapshot(a[1], a[2])
		if err != nil {
			bcliDie(err)
		}
		fmt.Println("回收对象", n)
	case "portable":
		need(4)
		repo := &backupRepository{paths: bcliPathsFor(userData(a[0])), schemaVersion: schema}
		if err := repo.exportPortable(a[1], a[3], a[2]); err != nil {
			bcliDie(err)
		}
	case "init-ss": // <srcRoot> <pw> <userData>：创建仓库并用系统密钥环（<userData>/Local State）包装仓库密钥
		need(3)
		repo := &backupRepository{paths: bcliPathsFor(userData(a[0])), schemaVersion: schema, protector: newKeyProtector(a[2])}
		if err := repo.initialize(a[1]); err != nil {
			bcliDie(err)
		}
	case "status": // <srcRoot> <userData> <out.json>
		need(3)
		repo := &backupRepository{paths: bcliPathsFor(userData(a[0])), schemaVersion: schema, protector: newKeyProtector(a[1])}
		bcliWriteJSON(a[2], repo.status())
	case "ss-key": // <srcRoot> <pw> <out>：仅测试用，输出仓库密钥的 base64
		need(3)
		repo := &backupRepository{paths: bcliPathsFor(userData(a[0])), schemaVersion: schema}
		if err := repo.unlockWithPassword(a[1]); err != nil {
			bcliDie(err)
		}
		os.WriteFile(a[2], []byte(base64.StdEncoding.EncodeToString(repo.cachedKey)), 0o644)
	case "read-auto": // <srcRoot> <snapshotId> <electronUserData> <out>
		need(4)
		repo := &backupRepository{paths: bcliPathsFor(userData(a[0])), schemaVersion: schema}
		repo.protector = newKeyProtector(a[2])
		if err := repo.unlockAutomatically(); err != nil {
			bcliDie(err)
		}
		m, err := repo.readManifest(a[1], "")
		if err != nil {
			bcliDie(err)
		}
		for _, e := range m.Entries {
			if _, err := repo.readEntry(e); err != nil {
				bcliDie(err)
			}
		}
		bcliWriteJSON(a[3], map[string]any{"entries": len(m.Entries), "id": m.ID})
	case "ss-decrypt": // <userData> <enc.b64> <out>
		need(3)
		raw, err := os.ReadFile(a[1])
		if err != nil {
			bcliDie(err)
		}
		blob, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
		if err != nil {
			bcliDie(err)
		}
		plain, err := safeStorageDecrypt(a[0], blob)
		if err != nil {
			bcliDie(err)
		}
		os.WriteFile(a[2], plain, 0o644)
	case "ss-encrypt": // <userData> <明文> <out.b64>
		need(3)
		blob, err := safeStorageEncrypt(a[0], []byte(a[1]))
		if err != nil {
			bcliDie(err)
		}
		os.WriteFile(a[2], []byte(base64.StdEncoding.EncodeToString(blob)), 0o644)
	default:
		bcliDie(fmt.Errorf("未知子命令 %s", cmd))
	}
	return 0
}
