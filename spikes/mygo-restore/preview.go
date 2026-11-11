package main

// 恢复预检，对应 backup-repository-preview.ts（不含主题阅读页资源核对，见 restore.go 的说明）。

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

type restorePreview struct {
	Success      bool          `json:"success"`
	Snapshot     *snapshotInfo `json:"snapshot,omitempty"`
	MissingFiles []string      `json:"missingFiles"`
	DamagedFiles []string      `json:"damagedFiles"`
	Warnings     []string      `json:"warnings"`
	Error        string        `json:"error,omitempty"`
}

func uniqueStrings(in []string) []string {
	out := []string{}
	for _, s := range in {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

func previewRepositoryRestore(repo *backupRepository, snapshotID, password string) restorePreview {
	missing, damaged, warnings := []string{}, []string{}, []string{}
	fail := func(err error) restorePreview {
		return restorePreview{MissingFiles: missing, DamagedFiles: damaged, Warnings: warnings, Error: err.Error()}
	}
	manifest, err := repo.readManifest(snapshotID, password)
	if err != nil {
		return fail(err)
	}
	var snap *snapshotInfo
	for _, s := range repo.listSnapshots() {
		if s.FileName == snapshotID {
			c := s
			snap = &c
			break
		}
	}
	if snap == nil {
		return fail(errors.New("找不到指定快照"))
	}
	var database []byte
	for _, e := range manifest.Entries {
		plain, err := repo.readEntry(e)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				missing = append(missing, e.LogicalPath)
			} else {
				damaged = append(damaged, e.LogicalPath)
			}
			continue
		}
		if e.Category == "database" {
			database = plain
		}
	}
	if database == nil {
		missing = append(missing, "data/knowledge.db")
	}
	if database != nil {
		tmp, err := os.MkdirTemp("", "guizhi-restore-preview-")
		if err != nil {
			return fail(err)
		}
		defer os.RemoveAll(tmp)
		dbPath := filepath.Join(tmp, "knowledge.db")
		if err := os.WriteFile(dbPath, database, 0o644); err != nil {
			return fail(err)
		}
		probe, err := openReadOnly(dbPath)
		if err != nil {
			return fail(err)
		}
		ok, qerr := quickCheckOK(probe)
		if qerr != nil || !ok {
			damaged = append(damaged, "data/knowledge.db")
		}
		ver, verr := userVersion(probe)
		probe.Close()
		if verr != nil {
			return fail(verr)
		}
		switch {
		case ver > repo.schemaVersion:
			damaged = append(damaged, "data/knowledge.db")
			warnings = append(warnings, fmt.Sprintf("快照数据结构 v%d 高于当前应用支持的 v%d", ver, repo.schemaVersion))
		case ver < repo.schemaVersion:
			warnings = append(warnings, fmt.Sprintf("恢复后将把数据结构从 v%d 迁移到 v%d", ver, repo.schemaVersion))
		}
	}
	valid := len(missing) == 0 && len(damaged) == 0
	snap.Validation = "invalid"
	if valid {
		snap.Validation = "valid"
	}
	p := restorePreview{Success: valid, Snapshot: snap, MissingFiles: uniqueStrings(missing), DamagedFiles: uniqueStrings(damaged), Warnings: warnings}
	if !valid {
		p.Error = "快照存在缺失或损坏文件，不能恢复"
	}
	return p
}
