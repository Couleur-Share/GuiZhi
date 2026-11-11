package main

// 恢复流程，对应 backup-repository-restore.ts（暂存 + 交换）。
// 与 TS 版一致的安全约束：全量解密到同盘暂存目录并再次校验数据库；换库前由调用方关闭数据库；
// 任一步失败都逆序恢复已移动的旧路径；机器相关的设置（可执行文件路径、开机启动等）保留本机当前值。
//
// 未覆盖：主题阅读页资源校验（inspectThemedReadingBackup / verifyThemedReadingBackupAssets，依赖主题阅读服务的移植）、
// 原文快照的 HTML/CSS 清理（TS 里也只是校验、结果丢弃——这里只移植了结构校验）。

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var restoreConfigFiles = map[string]bool{
	"ai-models.json": true, "illustration-styles.json": true, "shortcuts.json": true, "shortcut-mode.json": true, "mcp.json": true,
}

// 顺序与 TS 的 Set 字面量一致（影响合并后 JSON 里新增键的位置）
var machineBoundRendererKeys = []string{"dataPath", "launchAtStartup", "minimizeOnLaunch", "closeAction", "backgroundImageFileName", "ytDlpPath", "ffmpegPath", "backgroundTasksEnabled"}
var machineBoundDBKeys = []string{"launchAtStartup", "minimizeOnLaunch", "backgroundImageFileName", "ytDlpPath", "ffmpegPath", "backgroundTasksEnabled"}

type preparedRestore struct {
	StageDir                    string
	DatabasePath                string
	ImagesDir                   string
	VideosDir                   string
	ConfigDir                   string
	PendingRendererSettingsPath string
}

type restoreTargets struct {
	DatabasePath string
	ImagesDir    string
	VideosDir    string
	ConfigDir    string
}

var safeAssetPathRe = regexp.MustCompile(`^data/assets/(images|videos)/[A-Za-z0-9_.-]+$`)

func assertSafeAssetPath(logical string) error {
	if !safeAssetPathRe.MatchString(logical) || strings.Contains(logical, "/../") {
		return fmt.Errorf("快照包含不安全的媒体路径: %s", logical)
	}
	return nil
}

func mergeRendererSettings(restored, current *omap) *omap {
	for _, k := range machineBoundRendererKeys {
		if current != nil {
			if v, ok := current.get(k); ok {
				restored.set(k, v)
				continue
			}
		}
		restored.delete(k)
	}
	return restored
}

var sha256HexRe = regexp.MustCompile(`^[a-f0-9]{64}$`)

// validateSnapshotFormat 对应 sanitizeSnapshot 的结构校验部分（抛错的那几条）。
func validateSnapshotFormat(raw json.RawMessage) error {
	var s map[string]any
	if json.Unmarshal(raw, &s) != nil || s == nil {
		return errors.New("原文快照格式无效")
	}
	html, hok := s["html"].(string)
	css, cok := s["css"].(string)
	assets, aok := s["assets"].([]any)
	if v, _ := s["formatVersion"].(float64); v != 1 || !hok || !cok || utf16Len(html)+utf16Len(css) > 12*1024*1024 || !aok || len(assets) > 200 {
		return errors.New("原文快照格式无效")
	}
	failures, fok := s["failures"].([]any)
	warnings, wok := s["warnings"].([]any)
	_, accOK := s["account"].(string)
	_, autOK := s["author"].(string)
	bad := !fok || len(failures) > 10000 || !wok || !accOK || !autOK
	if !bad {
		for _, f := range failures {
			m, ok := f.(map[string]any)
			if !ok {
				bad = true
				break
			}
			if _, ok := m["url"].(string); !ok {
				bad = true
				break
			}
			if _, ok := m["reason"].(string); !ok {
				bad = true
				break
			}
		}
		for _, w := range warnings {
			if _, ok := w.(string); !ok {
				bad = true
				break
			}
		}
	}
	if bad {
		return errors.New("原文快照元数据无效")
	}
	for _, a := range assets {
		m, ok := a.(map[string]any)
		fn, _ := m["fileName"].(string)
		sh, _ := m["sha256"].(string)
		if !ok || !wechatAsset.MatchString(fn) || !sha256HexRe.MatchString(sh) {
			return errors.New("原文快照资源清单无效")
		}
	}
	return nil
}

// liveSetting 读取当前运行库里的一个设置；第二个返回值表示是否存在。
type liveSetting func(key string) (string, bool, error)

func copyMachineSettings(live liveSetting, stagedDB, stagedImagesDir string, schemaVersion int) error {
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(stagedDB)+"?_pragma=busy_timeout(10000)")
	if err != nil {
		return err
	}
	defer db.Close()
	for _, key := range machineBoundDBKeys {
		v, ok, err := live(key)
		if err != nil {
			return err
		}
		if ok {
			_, err = db.Exec("INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)", key, v)
		} else {
			_, err = db.Exec("DELETE FROM settings WHERE key = ?", key)
		}
		if err != nil {
			return err
		}
	}
	var name string
	if db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='web_source_versions'").Scan(&name) == nil {
		payloads, err := queryStrings(db, "SELECT payload FROM web_source_versions")
		if err != nil {
			return err
		}
		if err := validateSourceVersionPayloads(payloads); err != nil {
			return err
		}
	}
	ok, err := quickCheckOK(db)
	if err != nil || !ok {
		return errors.New("恢复数据库 quick_check 未通过")
	}
	ver, err := userVersion(db)
	if err != nil {
		return err
	}
	if ver > schemaVersion {
		return fmt.Errorf("快照数据结构 v%d 高于当前支持的 v%d", ver, schemaVersion)
	}
	return nil
}

// prepareRepositoryRestore 全量解密到同一 userData 文件系统上的暂存目录，并再次校验数据库。
func prepareRepositoryRestore(repo *backupRepository, snapshotID, password string, live liveSetting, targets restoreTargets, currentRenderer *omap) (_ *preparedRestore, err error) {
	userData := filepath.Dir(filepath.Dir(targets.DatabasePath))
	stageDir, err := os.MkdirTemp(userData, ".restore-stage-")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(stageDir)
		}
	}()
	stagedDB := filepath.Join(stageDir, "data", "knowledge.db")
	stagedImages := filepath.Join(stageDir, "data", "assets", "images")
	stagedVideos := filepath.Join(stageDir, "data", "assets", "videos")
	stagedConfig := filepath.Join(stageDir, "config")
	pending := ""

	manifest, err := repo.readManifest(snapshotID, password)
	if err != nil {
		return nil, err
	}
	for _, e := range manifest.Entries {
		plain, err := repo.readEntry(e)
		if err != nil {
			return nil, err
		}
		var dest string
		switch e.Category {
		case "database":
			if e.LogicalPath != "data/knowledge.db" {
				return nil, errors.New("数据库快照路径不合法")
			}
			dest = stagedDB
		case "media":
			if err := assertSafeAssetPath(e.LogicalPath); err != nil {
				return nil, err
			}
			dest = filepath.Join(append([]string{stageDir}, strings.Split(e.LogicalPath, "/")...)...)
		case "config":
			name := path.Base(e.LogicalPath)
			if !restoreConfigFiles[name] || e.LogicalPath != "config/"+name {
				return nil, fmt.Errorf("配置快照路径不合法: %s", e.LogicalPath)
			}
			dest = filepath.Join(stagedConfig, name)
		default:
			parsed, err := parseOrdered(plain)
			if err != nil {
				return nil, err
			}
			restored, ok := parsed.(*omap)
			if !ok {
				return nil, errors.New("渲染进程设置快照格式无效")
			}
			merged := mergeRendererSettings(restored, currentRenderer)
			pending = filepath.Join(stagedConfig, "pending-renderer-settings.json")
			if err := os.MkdirAll(filepath.Dir(pending), 0o755); err != nil {
				return nil, err
			}
			data, err := marshalNoEscape(merged)
			if err != nil {
				return nil, err
			}
			if err := os.WriteFile(pending, data, 0o644); err != nil {
				return nil, err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(dest, plain, 0o644); err != nil {
			return nil, err
		}
	}
	if _, err := os.Stat(stagedDB); err != nil {
		return nil, errors.New("快照缺少数据库")
	}
	for _, d := range []string{stagedImages, stagedVideos, stagedConfig} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	if err := copyMachineSettings(live, stagedDB, stagedImages, repo.schemaVersion); err != nil {
		return nil, err
	}
	return &preparedRestore{StageDir: stageDir, DatabasePath: stagedDB, ImagesDir: stagedImages, VideosDir: stagedVideos, ConfigDir: stagedConfig, PendingRendererSettingsPath: pending}, nil
}

type moveRecord struct {
	target   string
	rollback string
}

// applyHook 仅供测试：在第 n 次替换之后注入失败，验证回滚。
var applyHook func(replaced int) error

func replacePath(incoming, target, rollbackDir string, records *[]moveRecord) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	rollback := ""
	if _, err := os.Lstat(target); err == nil {
		rollback = filepath.Join(rollbackDir, fmt.Sprintf("%d-%s", len(*records), filepath.Base(target)))
		if err := os.Rename(target, rollback); err != nil {
			return err
		}
	}
	if err := os.Rename(incoming, target); err != nil {
		if rollback != "" {
			if _, serr := os.Lstat(rollback); serr == nil {
				_ = os.Rename(rollback, target)
			}
		}
		return err
	}
	*records = append(*records, moveRecord{target, rollback})
	if applyHook != nil {
		return applyHook(len(*records))
	}
	return nil
}

// applyPreparedRepositoryRestore 数据库已关闭后执行同盘交换；任一步失败都逆序恢复已移动的旧路径。
// pre-restore 数据库快照由调用方在关闭数据库前另行创建。
func applyPreparedRepositoryRestore(p *preparedRestore, t restoreTargets) (err error) {
	userData := filepath.Dir(filepath.Dir(t.DatabasePath))
	rollbackDir := filepath.Join(userData, ".restore-rollback-"+newUUID())
	if err := os.MkdirAll(rollbackDir, 0o755); err != nil {
		return err
	}
	var records []moveRecord
	defer func() {
		os.RemoveAll(p.StageDir)
		os.RemoveAll(rollbackDir)
	}()
	run := func() error {
		if err := replacePath(p.DatabasePath, t.DatabasePath, rollbackDir, &records); err != nil {
			return err
		}
		if err := replacePath(p.ImagesDir, t.ImagesDir, rollbackDir, &records); err != nil {
			return err
		}
		if err := replacePath(p.VideosDir, t.VideosDir, rollbackDir, &records); err != nil {
			return err
		}
		for _, name := range []string{"ai-models.json", "illustration-styles.json", "shortcuts.json", "shortcut-mode.json", "mcp.json"} {
			incoming := filepath.Join(p.ConfigDir, name)
			if _, err := os.Stat(incoming); err == nil {
				if err := replacePath(incoming, filepath.Join(t.ConfigDir, name), rollbackDir, &records); err != nil {
					return err
				}
			}
		}
		pending := filepath.Join(p.ConfigDir, "pending-renderer-settings.json")
		if _, err := os.Stat(pending); err == nil {
			if err := replacePath(pending, filepath.Join(t.ConfigDir, "pending-renderer-settings.json"), rollbackDir, &records); err != nil {
				return err
			}
		}
		for _, suffix := range []string{"-wal", "-shm", "-journal", ".clean"} {
			os.Remove(t.DatabasePath + suffix)
		}
		return nil
	}
	if err := run(); err != nil {
		for i := len(records) - 1; i >= 0; i-- {
			r := records[i]
			os.RemoveAll(r.target)
			if r.rollback != "" {
				if _, serr := os.Lstat(r.rollback); serr == nil {
					_ = os.Rename(r.rollback, r.target)
				}
			}
		}
		return err
	}
	return nil
}

// delete 删除键并保持其余键序（JS 的 delete obj[k]）。
func (m *omap) delete(k string) {
	if _, ok := m.vals[k]; !ok {
		return
	}
	delete(m.vals, k)
	for i, key := range m.keys {
		if key == k {
			m.keys = append(m.keys[:i], m.keys[i+1:]...)
			break
		}
	}
}

// rawTruthy 对应 JS 的真值判断（用于 `if (version.snapshot)`）：null/false/0/"" 为假。
func rawTruthy(raw json.RawMessage) bool {
	switch s := string(raw); s {
	case "", "null", "false", "0", `""`:
		return false
	}
	return true
}

// validateSourceVersionPayloads 对应 backup 校验里逐条 web_source_versions 的 JSON.parse + sanitizeSnapshot（只移植结构校验）。
func validateSourceVersionPayloads(payloads []string) error {
	for _, p := range payloads {
		var v struct {
			Snapshot json.RawMessage `json:"snapshot"`
		}
		if err := json.Unmarshal([]byte(p), &v); err != nil {
			return err
		}
		if rawTruthy(v.Snapshot) {
			if err := validateSnapshotFormat(v.Snapshot); err != nil {
				return err
			}
		}
	}
	return nil
}
