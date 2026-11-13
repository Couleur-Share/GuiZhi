package main

// 加密备份仓库，对应 apps/desktop/src/main/services/backup-repository.ts。
//
// 磁盘格式（与 TS 版逐字节兼容，两边可以互读互写同一个仓库）：
//   repository-header.json            口令包装的仓库密钥（scrypt + AES-256-GCM），可选 safeStorage 包装
//   objects/<sha256 明文>              AES-256-GCM 加密的对象（媒体不压缩，其余先 gzip）
//   snapshots/<id>/header.json         明文快照头（列表用）
//   snapshots/<id>/manifest.enc        加密的 gzip(JSON) 清单
//
// 与 TS 版不同的两处：
//   - 数据库快照用 modernc 只读连接执行 VACUUM INTO（MyGo 内置 SQLite 会拒绝，见迁移文档「备份验证」）。
//   - safeStorage（Electron 的 DPAPI 封装）包装的密钥：这里只保留字段、不解开；无人值守解锁需要单独迁移。

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	_ "modernc.org/sqlite"
)

const repositoryVersion = 1

var configFileNames = []string{"ai-models.json", "illustration-styles.json", "shortcuts.json", "shortcut-mode.json", "mcp.json"}

type repositoryHeader struct {
	Kind                  string             `json:"kind"`
	Version               int                `json:"version"`
	CreatedAt             int64              `json:"createdAt"`
	PasswordWrappedKey    passwordWrappedKey `json:"passwordWrappedKey"`
	SafeStorageWrappedKey *string            `json:"safeStorageWrappedKey"`
	KeyStorageBackend     *string            `json:"keyStorageBackend"`
}

type snapshotEntry struct {
	LogicalPath string `json:"logicalPath"`
	Category    string `json:"category"` // database | config | renderer-settings | media
	Sha256      string `json:"sha256"`
	SizeBytes   int64  `json:"sizeBytes"`
	StoredBytes int64  `json:"storedBytes"`
	Compression string `json:"compression"` // gzip | none
}

type snapshotSummary struct {
	ItemCount     int      `json:"itemCount"`
	AssetCount    int      `json:"assetCount"`
	ConfigDomains []string `json:"configDomains"`
}

type snapshotManifest struct {
	Kind          string          `json:"kind"`
	Version       int             `json:"version"`
	ID            string          `json:"id"`
	CreatedAt     int64           `json:"createdAt"`
	AppVersion    string          `json:"appVersion"`
	SchemaVersion int             `json:"schemaVersion"`
	Entries       []snapshotEntry `json:"entries"`
	Summary       snapshotSummary `json:"summary"`
}

type snapshotHeader struct {
	Kind           string   `json:"kind"`
	Version        int      `json:"version"`
	ID             string   `json:"id"`
	CreatedAt      int64    `json:"createdAt"`
	AppVersion     string   `json:"appVersion"`
	SchemaVersion  int      `json:"schemaVersion"`
	ItemCount      int      `json:"itemCount"`
	AssetCount     int      `json:"assetCount"`
	ConfigDomains  []string `json:"configDomains"`
	ManifestSha256 string   `json:"manifestSha256"`
	SizeBytes      int64    `json:"sizeBytes"`
	BackupKind     string   `json:"backupKind,omitempty"`
}

// snapshotInfo 对应 BackupFileInfo（repository-snapshot 形态）。
type snapshotInfo struct {
	FileName   string `json:"fileName"`
	Path       string `json:"path,omitempty"`
	Kind       string `json:"kind"`
	Format     string `json:"format"`
	SizeBytes  int64  `json:"sizeBytes"`
	CreatedAt  int64  `json:"createdAt"`
	Encrypted  bool   `json:"encrypted"`
	Validation string `json:"validation"`
	Summary    struct {
		ItemCount     int      `json:"itemCount"`
		AssetCount    int      `json:"assetCount"`
		ConfigDomains []string `json:"configDomains"`
		SchemaVersion int      `json:"schemaVersion"`
		AppVersion    string   `json:"appVersion"`
	} `json:"summary"`
}

type repoPaths struct {
	RepositoryDir string
	DatabasePath  string
	ConfigDir     string
	ImagesDir     string
	VideosDir     string
}

type backupRepository struct {
	paths     repoPaths
	cachedKey []byte
	// schemaVersion 是当前应用支持的数据库结构版本（TS 里是 SCHEMA_VERSION，迁移列表的长度）
	schemaVersion int
	// protector 是系统密钥环适配（Windows：DPAPI，经 Local State）；nil 表示不可用
	protector keyProtector
}

// keyProtector 对应 TS 的 RepositoryKeyProtector。
type keyProtector interface {
	Backend() string
	Available() bool
	Secure() bool
	// Wrap 返回的密文内容是 base64(key) 文本（与 safeStorage.encryptString(key.toString("base64")) 一致）
	Wrap(key []byte) ([]byte, error)
	Unwrap(wrapped []byte) ([]byte, error)
}

// repoStatus 对应 BackupRepositoryStatus。
type repoStatus struct {
	Initialized              bool    `json:"initialized"`
	AutomaticAccessAvailable bool    `json:"automaticAccessAvailable"`
	KeyStorageBackend        *string `json:"keyStorageBackend"`
	Warning                  string  `json:"warning,omitempty"`
}

const keyringUnavailableWarning = "系统安全密钥环不可用，自动完整备份已暂停；请配置系统密钥环后重新授权"

func (r *backupRepository) protectorUsable() bool {
	return r.protector != nil && r.protector.Available() && r.protector.Secure()
}

// status 对应 TS 的 status()。与 TS 的差异：额外试解一次密钥，Local State 被删/换机器后如实报告不可用（TS 只看 safeStorage 是否可加密）。
func (r *backupRepository) status() repoStatus {
	if _, err := os.Stat(r.headerPath()); err != nil {
		return repoStatus{}
	}
	h, err := r.readHeader()
	if err != nil {
		return repoStatus{Initialized: true, Warning: err.Error()}
	}
	auto := false
	if h.SafeStorageWrappedKey != nil && r.protectorUsable() {
		if wrapped, derr := base64.StdEncoding.DecodeString(*h.SafeStorageWrappedKey); derr == nil {
			_, uerr := r.protector.Unwrap(wrapped)
			auto = uerr == nil
		}
	}
	st := repoStatus{Initialized: true, AutomaticAccessAvailable: auto, KeyStorageBackend: h.KeyStorageBackend}
	if !auto {
		st.Warning = keyringUnavailableWarning
	}
	return st
}

// pruneAutoSnapshots 只保留最近 keepCount 份自动快照（手动快照不动），返回删除数。
func (r *backupRepository) pruneAutoSnapshots(keepCount int) (int, error) {
	var autos []snapshotInfo
	for _, s := range r.listSnapshots() {
		if s.Kind == "auto" {
			autos = append(autos, s)
		}
	}
	keep := max(1, keepCount)
	if len(autos) <= keep {
		return 0, nil
	}
	excess := autos[keep:]
	for _, s := range excess {
		dir, err := r.resolveSnapshotDir(s.FileName)
		if err != nil {
			return 0, err
		}
		if err := os.RemoveAll(dir); err != nil {
			return 0, err
		}
	}
	if _, err := r.garbageCollect(); err != nil {
		return len(excess), err
	}
	return len(excess), nil
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp-" + newUUID()
	defer os.Remove(tmp)
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func gzipBytes(b []byte) []byte {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	w.Write(b)
	w.Close()
	return buf.Bytes()
}

func gunzipBytes(b []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

func (r *backupRepository) headerPath() string {
	return filepath.Join(r.paths.RepositoryDir, "repository-header.json")
}
func (r *backupRepository) objectsDir() string {
	return filepath.Join(r.paths.RepositoryDir, "objects")
}
func (r *backupRepository) snapshotsDir() string {
	return filepath.Join(r.paths.RepositoryDir, "snapshots")
}

func (r *backupRepository) readHeader() (*repositoryHeader, error) {
	raw, err := os.ReadFile(r.headerPath())
	if err != nil {
		return nil, err
	}
	var h repositoryHeader
	if err := json.Unmarshal(raw, &h); err != nil {
		return nil, err
	}
	if h.Kind != "guizhi-backup-repository" || h.Version != repositoryVersion || h.PasswordWrappedKey.Salt == "" || h.PasswordWrappedKey.Data == "" {
		return nil, errors.New("备份仓库头不合法或版本不受支持")
	}
	return &h, nil
}

func (r *backupRepository) initialize(password string) error {
	if _, err := os.Stat(r.headerPath()); err == nil {
		return errors.New("备份仓库已经初始化")
	}
	key, err := createRepositoryKey()
	if err != nil {
		return err
	}
	wrapped, err := wrapKeyWithPassword(key, password)
	if err != nil {
		return err
	}
	h := repositoryHeader{Kind: "guizhi-backup-repository", Version: repositoryVersion, CreatedAt: time.Now().UnixMilli(), PasswordWrappedKey: wrapped}
	if r.protectorUsable() {
		sealed, err := r.protector.Wrap(key)
		if err != nil {
			return err
		}
		b64, backend := base64.StdEncoding.EncodeToString(sealed), r.protector.Backend()
		h.SafeStorageWrappedKey, h.KeyStorageBackend = &b64, &backend
	}
	if err := os.MkdirAll(r.objectsDir(), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(r.snapshotsDir(), 0o755); err != nil {
		return err
	}
	raw, _ := json.MarshalIndent(h, "", "  ")
	if err := atomicWrite(r.headerPath(), raw); err != nil {
		return err
	}
	r.cachedKey = key
	return nil
}

func (r *backupRepository) unlockWithPassword(password string) error {
	h, err := r.readHeader()
	if err != nil {
		return err
	}
	key, err := unwrapKeyWithPassword(h.PasswordWrappedKey, password)
	if err != nil {
		return err
	}
	r.cachedKey = key
	return nil
}

// unlockAutomatically 对应 TS 的无人值守解锁：用系统密钥环（Windows：Local State 里受 DPAPI 保护的主密钥，与 Electron 版同一份）解开仓库密钥。
func (r *backupRepository) unlockAutomatically() error {
	h, err := r.readHeader()
	if err != nil {
		return err
	}
	if h.SafeStorageWrappedKey == nil || r.protector == nil || !r.protector.Available() || !r.protector.Secure() {
		return errors.New("系统安全密钥环不可用，无法执行无人值守完整备份")
	}
	wrapped, err := base64.StdEncoding.DecodeString(*h.SafeStorageWrappedKey)
	if err != nil {
		return err
	}
	key, err := r.protector.Unwrap(wrapped)
	if err != nil {
		return err
	}
	r.cachedKey = key
	return nil
}
func (r *backupRepository) changePassword(current, next string) error {
	h, err := r.readHeader()
	if err != nil {
		return err
	}
	key, err := unwrapKeyWithPassword(h.PasswordWrappedKey, current)
	if err != nil {
		return err
	}
	wrapped, err := wrapKeyWithPassword(key, next)
	if err != nil {
		return err
	}
	h.PasswordWrappedKey = wrapped
	raw, _ := json.MarshalIndent(h, "", "  ")
	if err := atomicWrite(r.headerPath(), raw); err != nil {
		return err
	}
	r.cachedKey = key
	return nil
}

func (r *backupRepository) requireKey() ([]byte, error) {
	if r.cachedKey == nil {
		if err := r.unlockAutomatically(); err != nil {
			return nil, err
		}
	}
	if len(r.cachedKey) != keyBytes {
		return nil, errors.New("无法解锁备份仓库")
	}
	return r.cachedKey, nil
}

var snapshotIDRe = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

func (r *backupRepository) resolveSnapshotDir(id string) (string, error) {
	if !snapshotIDRe.MatchString(id) {
		return "", errors.New("快照 id 不合法")
	}
	dir := filepath.Join(r.snapshotsDir(), id)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return "", errors.New("找不到指定快照")
	}
	return dir, nil
}

func (r *backupRepository) headerToInfo(h snapshotHeader) snapshotInfo {
	info := snapshotInfo{
		FileName: h.ID, Path: filepath.Join(r.snapshotsDir(), h.ID), Kind: h.BackupKind, Format: "repository-snapshot",
		SizeBytes: h.SizeBytes, CreatedAt: h.CreatedAt, Encrypted: true, Validation: "unchecked",
	}
	if info.Kind == "" {
		info.Kind = "auto"
	}
	info.Summary.ItemCount, info.Summary.AssetCount, info.Summary.ConfigDomains = h.ItemCount, h.AssetCount, h.ConfigDomains
	if info.Summary.ConfigDomains == nil {
		info.Summary.ConfigDomains = []string{}
	}
	info.Summary.SchemaVersion, info.Summary.AppVersion = h.SchemaVersion, h.AppVersion
	return info
}

func (r *backupRepository) listSnapshots() []snapshotInfo {
	out := []snapshotInfo{}
	entries, err := os.ReadDir(r.snapshotsDir())
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(r.snapshotsDir(), e.Name(), "header.json"))
		if err != nil {
			continue // 半成品/损坏目录不冒充可恢复快照
		}
		var h snapshotHeader
		if json.Unmarshal(raw, &h) != nil || h.Kind != "guizhi-repository-snapshot-header" {
			continue
		}
		out = append(out, r.headerToInfo(h))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out
}

func (r *backupRepository) readManifest(id, password string) (*snapshotManifest, error) {
	if password != "" {
		if err := r.unlockWithPassword(password); err != nil {
			return nil, err
		}
	}
	key, err := r.requireKey()
	if err != nil {
		return nil, err
	}
	dir, err := r.resolveSnapshotDir(id)
	if err != nil {
		return nil, err
	}
	rawHeader, err := os.ReadFile(filepath.Join(dir, "header.json"))
	if err != nil {
		return nil, err
	}
	var h snapshotHeader
	if err := json.Unmarshal(rawHeader, &h); err != nil {
		return nil, err
	}
	enc, err := os.ReadFile(filepath.Join(dir, "manifest.enc"))
	if err != nil {
		return nil, err
	}
	if sha256Hex(enc) != h.ManifestSha256 {
		return nil, errors.New("快照 manifest 校验失败")
	}
	plain, err := decryptBuffer(enc, key)
	if err != nil {
		return nil, err
	}
	plain, err = gunzipBytes(plain)
	if err != nil {
		return nil, err
	}
	var m snapshotManifest
	if err := json.Unmarshal(plain, &m); err != nil {
		return nil, err
	}
	if m.ID != id || m.Kind != "guizhi-repository-snapshot" {
		return nil, errors.New("快照 manifest 与目录不匹配")
	}
	return &m, nil
}

func (r *backupRepository) readEntry(e snapshotEntry) ([]byte, error) {
	key, err := r.requireKey()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(r.objectsDir(), e.Sha256))
	if err != nil {
		return nil, err
	}
	plain, err := decryptBuffer(raw, key)
	if err != nil {
		return nil, err
	}
	if e.Compression == "gzip" {
		if plain, err = gunzipBytes(plain); err != nil {
			return nil, err
		}
	}
	if int64(len(plain)) != e.SizeBytes || sha256Hex(plain) != e.Sha256 {
		return nil, fmt.Errorf("对象内容校验失败: %s", e.LogicalPath)
	}
	return plain, nil
}

func (r *backupRepository) storeObject(plain []byte, compression string, key []byte) (snapshotEntry, bool, error) {
	hash := sha256Hex(plain)
	objPath := filepath.Join(r.objectsDir(), hash)
	if st, err := os.Stat(objPath); err == nil {
		return snapshotEntry{Sha256: hash, SizeBytes: int64(len(plain)), StoredBytes: st.Size(), Compression: compression}, true, nil
	}
	prepared := plain
	if compression == "gzip" {
		prepared = gzipBytes(plain)
	}
	enc, err := encryptBuffer(prepared, key)
	if err != nil {
		return snapshotEntry{}, false, err
	}
	if err := atomicWrite(objPath, enc); err != nil {
		return snapshotEntry{}, false, err
	}
	return snapshotEntry{Sha256: hash, SizeBytes: int64(len(plain)), StoredBytes: int64(len(enc)), Compression: compression}, false, nil
}

// deleteSnapshot / garbageCollect 对应 TS：删快照目录后，回收不再被任何快照引用的对象。
func (r *backupRepository) deleteSnapshot(id, password string) (int, error) {
	if password != "" {
		if err := r.unlockWithPassword(password); err != nil {
			return 0, err
		}
	}
	dir, err := r.resolveSnapshotDir(id)
	if err != nil {
		return 0, err
	}
	if err := os.RemoveAll(dir); err != nil {
		return 0, err
	}
	return r.garbageCollect()
}

var objectNameRe = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (r *backupRepository) garbageCollect() (int, error) {
	referenced := map[string]bool{}
	for _, s := range r.listSnapshots() {
		m, err := r.readManifest(s.FileName, "")
		if err != nil {
			return 0, err // 读不出清单就不能确认引用关系，宁可不删
		}
		for _, e := range m.Entries {
			referenced[e.Sha256] = true
		}
	}
	names, err := os.ReadDir(r.objectsDir())
	if err != nil {
		return 0, nil
	}
	removed := 0
	for _, n := range names {
		if objectNameRe.MatchString(n.Name()) && !referenced[n.Name()] {
			if os.Remove(filepath.Join(r.objectsDir(), n.Name())) == nil {
				removed++
			}
		}
	}
	return removed, nil
}

// ---- 创建快照 ----

type snapshotRequest struct {
	RecoveryPassword string
	RendererSettings *omap
	Kind             string
	AppVersion       string
}

type snapshotResult struct {
	Success        bool          `json:"success"`
	Error          string        `json:"error,omitempty"`
	Snapshot       *snapshotInfo `json:"snapshot,omitempty"`
	ReusedObjects  int           `json:"reusedObjects"`
	CreatedObjects int           `json:"createdObjects"`
}

var wechatAsset = regexp.MustCompile(`^wechat-[a-f0-9]{64}\.(png|jpg|gif|webp)$`)

// extractLocalAssetRefs 对应 shared/utils/media-refs.ts 的 extractLocalAssetRefs：只取指定协议（image / video），去重保序。
func extractLocalAssetRefs(content, protocol string) []string {
	var refs []string
	seen := map[string]bool{}
	for _, m := range assetRefPattern.FindAllStringSubmatch(content, -1) {
		if m[1] == protocol && isSafeAssetFileName(m[2]) && !seen[m[2]] {
			seen[m[2]] = true
			refs = append(refs, m[2])
		}
	}
	return refs
}
func openReadOnly(path string) (*sql.DB, error) {
	return sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro&_pragma=busy_timeout(10000)")
}

func quickCheckOK(db *sql.DB) (bool, error) {
	rows, err := db.Query("PRAGMA quick_check")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	var results []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return false, err
		}
		results = append(results, s)
	}
	return len(results) == 1 && results[0] == "ok", rows.Err()
}

func userVersion(db *sql.DB) (int, error) {
	var v int
	err := db.QueryRow("PRAGMA user_version").Scan(&v)
	return v, err
}

func queryStrings(db *sql.DB, q string) ([]string, error) {
	rows, err := db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// vacuumInto 用 modernc 只读连接对在线库做 VACUUM INTO。
func vacuumInto(srcPath, dstPath string) error {
	db, err := openReadOnly(srcPath)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec("VACUUM INTO ?", dstPath)
	return err
}

func (r *backupRepository) createSnapshot(req snapshotRequest) (res snapshotResult) {
	fail := func(err error) snapshotResult { return snapshotResult{Error: err.Error()} }
	if req.RecoveryPassword != "" {
		if err := r.unlockWithPassword(req.RecoveryPassword); err != nil {
			return fail(err)
		}
	}
	key, err := r.requireKey()
	if err != nil {
		return fail(err)
	}
	id := fmt.Sprintf("%d-%s", time.Now().UnixMilli(), newUUID())
	workDir, err := os.MkdirTemp(r.paths.RepositoryDir, ".snapshot-work-")
	if err != nil {
		return fail(err)
	}
	defer os.RemoveAll(workDir)
	snapDB := filepath.Join(workDir, "knowledge.db")
	if err := vacuumInto(r.paths.DatabasePath, snapDB); err != nil {
		return fail(err)
	}
	collected, err := r.collectEntries(snapDB, req.RendererSettings, key)
	if err != nil {
		return fail(err)
	}
	m := snapshotManifest{
		Kind: "guizhi-repository-snapshot", Version: repositoryVersion, ID: id, CreatedAt: time.Now().UnixMilli(),
		AppVersion: req.AppVersion, SchemaVersion: collected.schemaVersion, Entries: collected.entries,
		Summary: snapshotSummary{ItemCount: collected.itemCount, ConfigDomains: collected.configDomains},
	}
	for _, e := range m.Entries {
		if e.Category == "media" {
			m.Summary.AssetCount++
		}
	}
	manifestPlain, _ := json.Marshal(m)
	manifestEnc, err := encryptBuffer(gzipBytes(manifestPlain), key)
	if err != nil {
		return fail(err)
	}
	dir := filepath.Join(r.snapshotsDir(), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fail(err)
	}
	if err := atomicWrite(filepath.Join(dir, "manifest.enc"), manifestEnc); err != nil {
		return fail(err)
	}
	size := int64(len(manifestEnc))
	for _, e := range m.Entries {
		size += e.StoredBytes
	}
	kind := req.Kind
	if kind == "" {
		kind = "manual"
	}
	h := snapshotHeader{
		Kind: "guizhi-repository-snapshot-header", Version: repositoryVersion, ID: id, CreatedAt: m.CreatedAt, AppVersion: req.AppVersion,
		SchemaVersion: m.SchemaVersion, ItemCount: m.Summary.ItemCount, AssetCount: m.Summary.AssetCount, ConfigDomains: m.Summary.ConfigDomains,
		ManifestSha256: sha256Hex(manifestEnc), SizeBytes: size, BackupKind: kind,
	}
	raw, _ := json.MarshalIndent(h, "", "  ")
	if err := atomicWrite(filepath.Join(dir, "header.json"), raw); err != nil {
		return fail(err)
	}
	info := r.headerToInfo(h)
	return snapshotResult{Success: true, Snapshot: &info, ReusedObjects: collected.reused, CreatedObjects: collected.created}
}
