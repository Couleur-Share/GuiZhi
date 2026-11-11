package main

// 对应 apps/desktop/src/main/data-path.ts、shell-open-path.ts，以及 index.ts 里的
// dialog:selectFolder / data:getPath / data:getStatus / data:previewDataPathChange /
// data:applyDataPathChange / shell:openPath。
//
// 官方只支持 Windows，所以安装目录推断（install-scoped）只实现 win32 语义；其他平台与 TS 一样回落默认目录。
// 与 TS 的差异（已接受）：复制数据项时符号链接/联接点一律跳过（TS 的 fs.cpSync 会按链接原样复制）；
// 目录迁移失败的系统错误文案来自 Go 而非 Node。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/egoist/mygo"
)

const (
	dataPathConfigDir  = "GuiZhi"
	dataPathConfigFile = "data-path.json"
	legacyProductName  = "GuiZhi"
)

var dataMarkers = []string{"data", "config", "backups", "logs", "Local Storage", "Session Storage", "shortcuts.json", "shortcut-mode.json"}

type dataMarker struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
}

type dataPathInspection struct {
	TargetPath      string       `json:"targetPath"`
	Exists          bool         `json:"exists"`
	HasExistingData bool         `json:"hasExistingData"`
	Markers         []dataMarker `json:"markers"`
}

// nodeResolve 对应 Node 的 path.resolve（单参数、Windows 语义）：绝对化并规范化，去掉末尾分隔符。
func nodeResolve(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	return abs
}

// isLinkMode 判断是不是符号链接或联接点（Node 的 lstat 把两者都报为 symbolic link）。
func isLinkMode(m fs.FileMode) bool { return m&(fs.ModeSymlink|fs.ModeIrregular) != 0 }

// lstatSafe 对应 readLinkSafeStats：不存在返回 (nil, nil)，其他错误原样抛出。
func lstatSafe(p string) (fs.FileInfo, error) {
	st, err := os.Lstat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return st, nil
}

func dataPathConfigPath(appData string) string {
	return filepath.Join(appData, dataPathConfigDir, dataPathConfigFile)
}

func historicalDefaultUserDataPath(appData string) string {
	return filepath.Join(appData, legacyProductName)
}

// readConfiguredDataPath 对应同名函数：没有、格式不对或为空串都回 ""。
func readConfiguredDataPath(appData string) string {
	raw, err := os.ReadFile(dataPathConfigPath(appData))
	if err != nil {
		return ""
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		logAppErrorf("data-path", "读取数据目录配置", "%s", err.Error())
		return ""
	}
	s, ok := parsed["dataPath"].(string)
	if !ok || strings.TrimSpace(s) == "" {
		return ""
	}
	return nodeResolve(s)
}

func writeConfiguredDataPath(appData, dataPath string) error {
	p := dataPathConfigPath(appData)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	text, err := stringifyIndent2(newOmap().set("dataPath", nodeResolve(dataPath)).set("updatedAt", nowIso()))
	if err != nil {
		return err
	}
	return os.WriteFile(p, []byte(text), 0o644)
}

func isLinkSafeDataPathRoot(p string) (bool, error) {
	st, err := lstatSafe(nodeResolve(p))
	if err != nil {
		return false, err
	}
	return st != nil && st.IsDir() && !isLinkMode(st.Mode()), nil
}

func inspectDataPath(target string) (dataPathInspection, error) {
	resolved := nodeResolve(target)
	empty := dataPathInspection{TargetPath: resolved, Markers: []dataMarker{}}
	if target == "" {
		return empty, nil
	}
	st, err := lstatSafe(resolved)
	if err != nil {
		return empty, err
	}
	if st == nil {
		return empty, nil
	}
	empty.Exists = true
	if isLinkMode(st.Mode()) {
		return empty, nil
	}
	markers := []dataMarker{}
	for _, name := range dataMarkers {
		mp := filepath.Join(resolved, name)
		ms, err := lstatSafe(mp)
		if err != nil {
			return empty, err
		}
		if ms == nil || isLinkMode(ms.Mode()) {
			continue
		}
		typ := "other"
		if ms.IsDir() {
			typ = "directory"
		} else if ms.Mode().IsRegular() {
			typ = "file"
		}
		markers = append(markers, dataMarker{Name: name, Path: mp, Type: typ})
	}
	dataDir := filepath.Join(resolved, "data")
	dst, err := lstatSafe(dataDir)
	if err != nil {
		return empty, err
	}
	if dst != nil && dst.IsDir() && !isLinkMode(dst.Mode()) {
		dbPath := filepath.Join(dataDir, "knowledge.db")
		ks, err := lstatSafe(dbPath)
		if err != nil {
			return empty, err
		}
		if ks != nil && ks.Mode().IsRegular() {
			markers = append(markers, dataMarker{Name: "data/knowledge.db", Path: dbPath, Type: "file"})
		}
	}
	empty.Markers = markers
	empty.HasExistingData = len(markers) > 0
	return empty, nil
}

func hasExistingAppData(target string) bool {
	in, err := inspectDataPath(target)
	return err == nil && in.HasExistingData
}

func isPathWritable(p string) bool {
	st, err := os.Stat(p)
	if err != nil {
		return false
	}
	if !st.IsDir() {
		// Node 的 accessSync(W_OK) 对只读文件会失败，其余视为可写
		return st.Mode().Perm()&0o200 != 0
	}
	// 目录：尝试创建并删除一个探测文件（Windows 上目录的 W_OK 本身不可靠）
	f, err := os.CreateTemp(p, ".guizhi-write-probe-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}

func isProtectedInstallDir(target string) bool {
	n := strings.ToLower(strings.ReplaceAll(target, "/", "\\"))
	for _, seg := range []string{"\\windows\\", "\\program files\\", "\\program files (x86)\\"} {
		if strings.Contains(n, seg) {
			return true
		}
	}
	return false
}

func isDefaultPerUserInstallDir(target string) bool {
	return strings.Contains(strings.ToLower(strings.ReplaceAll(target, "/", "\\")), "\\appdata\\local\\programs\\")
}

type dataPathOptions struct {
	AppDataPath         string
	DefaultUserDataPath string
	ExePath             string
	IsPackaged          bool
	Platform            string
}

type dataPathDeps struct {
	readConfigured  func(appData string) string
	hasExistingData func(p string) bool
	isWritable      func(p string) bool
}

var defaultDataPathDeps = dataPathDeps{readConfigured: readConfiguredDataPath, hasExistingData: hasExistingAppData, isWritable: isPathWritable}

func win32Dir(p string) string {
	p = strings.ReplaceAll(p, "/", "\\")
	i := strings.LastIndex(p, "\\")
	switch {
	case i < 0:
		return "."
	case i == 0:
		return "\\"
	case i == 2 && len(p) >= 2 && p[1] == ':':
		return p[:3]
	}
	return p[:i]
}

func installScopedDataPath(exePath, platform string, isPackaged bool) string {
	if !isPackaged || platform != "win32" {
		return ""
	}
	installDir := win32Dir(strings.ReplaceAll(exePath, "/", "\\"))
	if isProtectedInstallDir(installDir) || isDefaultPerUserInstallDir(installDir) {
		return ""
	}
	return filepath.Join(installDir, "data")
}

// resolveInitialUserDataPath：自定义目录 > 历史默认目录（已有数据）> 安装目录旁的 data（已有数据且可写）> 历史默认目录。
func resolveInitialUserDataPath(o dataPathOptions, d dataPathDeps) string {
	if p := d.readConfigured(o.AppDataPath); p != "" {
		return p
	}
	if d.hasExistingData(o.DefaultUserDataPath) {
		return o.DefaultUserDataPath
	}
	if scoped := installScopedDataPath(o.ExePath, o.Platform, o.IsPackaged); scoped != "" && d.isWritable(win32Dir(scoped)) {
		if d.hasExistingData(scoped) {
			return scoped
		}
	}
	return o.DefaultUserDataPath
}

// ── 数据目录迁移 ──

type dataPathChangeResult struct {
	Success      bool    `json:"success"`
	Message      *string `json:"message,omitempty"`
	NewPath      *string `json:"newPath,omitempty"`
	NeedsRestart *bool   `json:"needsRestart,omitempty"`
	Error        *string `json:"error,omitempty"`
}

func strp(s string) *string { return &s }
func boolp(b bool) *bool    { return &b }

func dpFail(msg string) *dataPathChangeResult { return &dataPathChangeResult{Error: strp(msg)} }

func isSensitiveDataPathTarget(resolved string) bool {
	lower := strings.ToLower(resolved)
	for _, root := range []string{"/etc", "/usr", "/bin", "/sbin", "/var", "/tmp", "/System", "/Library", "C:\\Windows", "C:\\Program Files"} {
		if strings.HasPrefix(lower, strings.ToLower(root)) {
			return true
		}
	}
	return false
}

func isPathInside(parent, child string) bool {
	p, c := nodeResolve(parent), nodeResolve(child)
	return c != p && strings.HasPrefix(c, p+string(filepath.Separator))
}

// copyDataPathItemCp 对应 index.ts 里基于 fs.cpSync 的复制：递归合并；overwrite=false 时已存在的文件保留；
// 任何错误返回 false。符号链接跳过（见文件头说明）。
func copyDataPathItemCp(src, dst string, overwrite bool) bool {
	return copyTree(src, dst, overwrite) == nil
}

func copyTree(src, dst string, overwrite bool) error {
	st, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if isLinkMode(st.Mode()) {
		return nil
	}
	if st.IsDir() {
		if ds, err := os.Lstat(dst); err == nil && !ds.IsDir() {
			return fmt.Errorf("cannot overwrite non-directory %s with directory %s", dst, src)
		}
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name()), overwrite); err != nil {
				return err
			}
		}
		return nil
	}
	if ds, err := os.Lstat(dst); err == nil {
		if ds.IsDir() {
			return fmt.Errorf("cannot overwrite directory %s with non-directory %s", dst, src)
		}
		if !overwrite {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, st.Mode().Perm()|0o200)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func (l *Library) appDataPath() string {
	if l.appData != "" {
		return l.appData
	}
	return os.Getenv("APPDATA")
}

// countActiveImportTasks 对应 backup.ts 的同名函数：采集队列与站点爬取里仍在写入的任务数。
func (l *Library) countActiveImportTasks(ctx context.Context) (int64, error) {
	rows, err := l.db.Query(ctx, "SELECT COUNT(*) AS count FROM import_tasks WHERE status IN ('pending', 'processing')")
	if err != nil {
		return 0, err
	}
	var n int64
	if len(rows.Values) > 0 {
		n = asInt(rows.Values[0][0])
	}
	has, err := l.db.Query(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name='crawl_jobs'")
	if err != nil {
		return 0, err
	}
	if len(has.Values) == 0 {
		return n, nil
	}
	crawl, err := l.db.Query(ctx, "SELECT COUNT(*) AS count FROM crawl_jobs j WHERE j.status IN ('pending','running') OR EXISTS (SELECT 1 FROM crawl_pages p WHERE p.job_id=j.id AND p.status='running')")
	if err != nil {
		return 0, err
	}
	if len(crawl.Values) > 0 {
		n += asInt(crawl.Values[0][0])
	}
	return n, nil
}

func (l *Library) applyDataPathChange(ctx context.Context, newPath, action string) *dataPathChangeResult {
	if strings.TrimSpace(newPath) == "" {
		return dpFail("data path change requires a non-empty newPath string")
	}
	if action != "migrate" && action != "switch" && action != "overwrite" {
		return dpFail("Unsupported data path change action: " + action)
	}
	current := l.paths.UserData
	target := nodeResolve(newPath)
	if nodeResolve(current) == target {
		return &dataPathChangeResult{Success: true, Message: strp("Data directory is already current"), NewPath: strp(target), NeedsRestart: boolp(false)}
	}
	if isSensitiveDataPathTarget(target) {
		return dpFail("Cannot use system directory as data directory: " + target)
	}
	if _, err := os.Lstat(target); err == nil {
		safe, serr := isLinkSafeDataPathRoot(target)
		if serr != nil {
			return dpFail(serr.Error())
		}
		if !safe {
			return dpFail("Cannot use symbolic link as data directory: " + target)
		}
	}
	if action != "switch" && isPathInside(current, target) {
		return dpFail("Cannot migrate data into a child directory of the current data directory")
	}
	if l.hasActiveThemedReading != nil && l.hasActiveThemedReading() {
		return dpFail("主题排版正在执行，请先停止任务再切换数据目录")
	}
	inspection, err := inspectDataPath(target)
	if err != nil {
		return dpFail(err.Error())
	}
	active, err := l.countActiveImportTasks(ctx)
	if err != nil {
		return dpFail(err.Error())
	}
	if active > 0 {
		return dpFail("有采集任务正在写入，请先暂停或取消任务再切换数据目录")
	}
	if action == "switch" {
		if !inspection.Exists {
			return dpFail("Cannot switch to a directory that does not exist: " + target)
		}
		l.stopFrozenServices()
		if err := writeConfiguredDataPath(l.appDataPath(), target); err != nil {
			return dpFail(err.Error())
		}
		return &dataPathChangeResult{Success: true, Message: strp("Data directory switched"), NewPath: strp(target), NeedsRestart: boolp(true)}
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return dpFail(err.Error())
	}
	l.stopFrozenServices()
	migrated := 0
	for _, item := range dataMarkers {
		src := filepath.Join(current, item)
		if _, err := os.Lstat(src); err != nil {
			continue
		}
		if copyDataPathItemCp(src, filepath.Join(target, item), action == "overwrite") {
			migrated++
		}
	}
	l.stopFrozenServices()
	if err := writeConfiguredDataPath(l.appDataPath(), target); err != nil {
		return dpFail(err.Error())
	}
	return &dataPathChangeResult{Success: true, Message: strp(fmt.Sprintf("Successfully migrated %d items", migrated)), NewPath: strp(target), NeedsRestart: boolp(true)}
}

// stopFrozenServices 对应 stopMobileCapture(true) / stopReadingSync(true)：这两族功能已冻结，Go 壳层没有对应服务。
func (l *Library) stopFrozenServices() {}

// ── shell:openPath ──

type openPathDeps struct {
	appDataPath, homePath, localAppDataPath string
	lstat                                   func(string) (fs.FileInfo, error)
	stat                                    func(string) (fs.FileInfo, error)
	openPath                                func(string) error
	showItemInFolder                        func(string)
}

var (
	reTildePrefix  = regexp.MustCompile(`^~($|[/\\])`)
	reAppDataVar   = regexp.MustCompile(`(?i)%APPDATA%`)
	reLocalAppData = regexp.MustCompile(`(?i)%LOCALAPPDATA%`)
)

func expandShellOpenPath(p string, d openPathDeps) string {
	local := d.localAppDataPath
	if local == "" {
		local = filepath.Join(d.homePath, "AppData", "Local")
	}
	if m := reTildePrefix.FindStringSubmatchIndex(p); m != nil {
		p = d.homePath + p[m[2]:]
	}
	p = reAppDataVar.ReplaceAllLiteralString(p, d.appDataPath)
	return reLocalAppData.ReplaceAllLiteralString(p, local)
}

func openDirectoryPath(p string, d openPathDeps) map[string]any {
	if strings.TrimSpace(p) == "" {
		return map[string]any{"success": false, "error": "shell:openPath requires a non-empty folderPath string"}
	}
	real := expandShellOpenPath(p, d)
	lst, err := d.lstat(real)
	if err != nil {
		return map[string]any{"success": false, "error": "Directory does not exist or cannot be accessed"}
	}
	if isLinkMode(lst.Mode()) {
		d.showItemInFolder(real)
		return map[string]any{"success": true}
	}
	st, err := d.stat(real)
	if err != nil {
		return map[string]any{"success": false, "error": "Directory does not exist or cannot be accessed"}
	}
	if !st.IsDir() {
		d.showItemInFolder(real)
		return map[string]any{"success": true}
	}
	if err := d.openPath(real); err != nil {
		if msg := err.Error(); strings.TrimSpace(msg) != "" {
			return map[string]any{"success": false, "error": msg}
		}
	}
	return map[string]any{"success": true}
}

func (l *Library) registerDataPath(b *Bridge) {
	b.handlers["dialog:selectFolder"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		if l.pickDirectory == nil {
			return nil, nil
		}
		p, err := l.pickDirectory("选择数据目录")
		if err != nil || p == "" {
			return nil, nil
		}
		return p, nil
	}
	b.handlers["data:getPath"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		return l.paths.UserData, nil
	}
	b.handlers["data:getStatus"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		configured := readConfiguredDataPath(l.appDataPath())
		var cfg any
		if configured != "" {
			cfg = configured
		}
		return map[string]any{
			"configuredPath": cfg,
			"currentPath":    l.paths.UserData,
			"needsRestart":   configured != "" && nodeResolve(configured) != nodeResolve(l.paths.UserData),
		}, nil
	}
	b.handlers["data:previewDataPathChange"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		var newPath string
		var ok bool
		if len(a) > 0 {
			var v any
			if json.Unmarshal(a[0], &v) == nil {
				newPath, ok = v.(string)
			}
		}
		if !ok || strings.TrimSpace(newPath) == "" {
			return map[string]any{"success": false, "error": "data:previewDataPathChange requires a non-empty newPath string"}, nil
		}
		target := nodeResolve(newPath)
		if _, err := os.Lstat(target); err == nil {
			safe, serr := isLinkSafeDataPathRoot(target)
			if serr != nil {
				return nil, serr
			}
			if !safe {
				return map[string]any{"success": false, "error": "Cannot use symbolic link as data directory: " + target}, nil
			}
		}
		inspection, err := inspectDataPath(target)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"success": true, "targetPath": target, "currentPath": l.paths.UserData,
			"exists": inspection.Exists, "isCurrentPath": nodeResolve(l.paths.UserData) == target, "markers": inspection.Markers,
		}, nil
	}
	b.handlers["data:applyDataPathChange"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		var params map[string]any
		if len(a) > 0 {
			_ = json.Unmarshal(a[0], &params)
		}
		newPath, _ := params["newPath"].(string)
		action := "migrate"
		if s, ok := params["action"].(string); ok && (s == "switch" || s == "overwrite" || s == "migrate") {
			action = s
		}
		return l.applyDataPathChange(ctx, newPath, action), nil
	}
	b.handlers["shell:openPath"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		var p string
		if len(a) > 0 {
			var v any
			if json.Unmarshal(a[0], &v) == nil {
				p, _ = v.(string)
			}
		}
		home, _ := os.UserHomeDir()
		local := os.Getenv("LOCALAPPDATA")
		return openDirectoryPath(p, openPathDeps{
			appDataPath: l.appDataPath(), homePath: home, localAppDataPath: local,
			lstat: os.Lstat, stat: os.Stat,
			openPath:         func(s string) error { return mygo.Shell.OpenPath(s) },
			showItemInFolder: func(s string) { mygo.Shell.ShowItemInFolder(s) },
		}), nil
	}
}
