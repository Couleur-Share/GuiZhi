package main

// 对应 runtime-paths.ts、diagnostic-log.ts、ipc/app-runtime.ipc.ts、ipc/image.ipc.ts：
// 运行时路径、诊断日志（log:appError）、本地图片的选择/保存/读取。
//
// 已知差异：TS 的 getUserDataPath 在打包版里会经 data-path.ts 的 resolveInitialUserDataPath
// 选择便携目录或自定义数据目录；Go 壳层目前由 -userdata 直接给定，那一段随「目录迁移」切片再做。

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Paths 对应 runtime-paths.ts：所有目录都从 userData 派生。
type Paths struct{ UserData string }

func (p Paths) DataDir() string      { return filepath.Join(p.UserData, "data") }
func (p Paths) DatabasePath() string { return filepath.Join(p.DataDir(), "knowledge.db") }
func (p Paths) ConfigDir() string    { return filepath.Join(p.UserData, "config") }
func (p Paths) LogsDir() string      { return filepath.Join(p.UserData, "logs") }
func (p Paths) BackupsDir() string   { return filepath.Join(p.UserData, "backups") }
func (p Paths) ToolsDir() string     { return filepath.Join(p.UserData, "tools") }
func (p Paths) AssetsDir() string    { return filepath.Join(p.DataDir(), "assets") }
func (p Paths) ImagesDir() string    { return filepath.Join(p.AssetsDir(), "images") }
func (p Paths) VideosDir() string    { return filepath.Join(p.AssetsDir(), "videos") }
func (p Paths) AttachmentsDir() string {
	return filepath.Join(p.AssetsDir(), "attachments")
}

// ── 诊断日志 ──

const diagMaxLogBytes = 512 * 1024

func scrubMessage(value string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return value
	}
	// win32/darwin 大小写不敏感；本壳层官方只支持 Windows
	lowerHome, lower := strings.ToLower(home), strings.ToLower(value)
	var b strings.Builder
	cursor := 0
	for {
		at := strings.Index(lower[cursor:], lowerHome)
		if at < 0 {
			b.WriteString(value[cursor:])
			return b.String()
		}
		at += cursor
		b.WriteString(value[cursor:at])
		b.WriteByte('~')
		cursor = at + len(home)
	}
}

func rotateIfTooLarge(path string) {
	st, err := os.Stat(path)
	if err != nil || st.Size() <= diagMaxLogBytes {
		return
	}
	buf, err := os.ReadFile(path)
	if err != nil {
		return
	}
	half := len(buf) / 2
	start := half
	if i := indexByteFrom(buf, '\n', half); i >= 0 {
		start = i + 1
	}
	_ = os.WriteFile(path, buf[start:], 0o644)
}

func indexByteFrom(b []byte, c byte, from int) int {
	for i := from; i < len(b); i++ {
		if b[i] == c {
			return i
		}
	}
	return -1
}

var diagMu sync.Mutex

// appendLogLine 对应 appendLogLine：失败静默。
func (p Paths) appendLogLine(file string, entry *jsObj) {
	diagMu.Lock()
	defer diagMu.Unlock()
	path := filepath.Join(p.LogsDir(), file)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	rotateIfTooLarge(path)
	rec := newObj()
	rec.set("ts", time.Now().UTC().Format("2006-01-02T15:04:05.000Z"))
	rec.set("pid", float64(os.Getpid()))
	rec.set("version", appVersion)
	rec.set("platform", "win32")
	for _, k := range entry.keys() {
		rec.set(k, entry.get(k))
	}
	line, _ := jsStringify(rec)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line + "\n")
}

// logAppError 对应 diagnostic-log.ts 的同名函数。
func (p Paths) logAppError(scope, action, message string, extra *jsObj) {
	e := newObj()
	e.set("scope", scope)
	e.set("action", action)
	e.set("message", message)
	if extra != nil {
		for _, k := range extra.keys() {
			e.set(k, extra.get(k))
		}
	}
	e.set("message", scrubMessage(message))
	p.appendLogLine("error.log", e)
}

func readLogField(v any, max int) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return utf16Prefix(jsTrim(s), max)
}

// ── 图片 ──

var imageExtensions = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true}

var invalidFileNameRe = regexp.MustCompile("[/\\\\:\u0000-\u001f\u007f]")

// openDialogFn 打开「选择图片」对话框；取消返回空。可在测试里替换。
type openDialogFn func(multiple bool, filterName string, exts []string) ([]string, error)

type imageState struct {
	mu       sync.Mutex
	selected map[string]bool
}

func (s *imageState) set(paths []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.selected = map[string]bool{}
	for _, p := range paths {
		s.selected[resolvePath(p)] = true
	}
}

func (s *imageState) has(p string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.selected[p]
}

// resolvePath 对应 path.resolve：相对路径按当前工作目录解析。
func resolvePath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	return abs
}

// nodeExtname 对应 path.extname：最后一个点之后，起始的点不算。
func nodeExtname(p string) string {
	base := filepath.Base(p)
	i := strings.LastIndex(base, ".")
	if i <= 0 {
		return ""
	}
	return base[i:]
}

func validateImageFileName(name any, baseDir string) (string, error) {
	s, ok := name.(string)
	if !ok || s == "" || s == "." || s == ".." || strings.Contains(s, "..") || invalidFileNameRe.MatchString(s) {
		return "", errors.New("Invalid filename: path traversal detected")
	}
	// Node 用不做规范化的长路径访问文件：以空格或点结尾、或是设备名的名字在 Windows 上根本不存在，
	// 而 Go 走普通 Win32 路径会被规范化成别的文件或设备。
	if last := s[len(s)-1]; last == ' ' || last == '.' || windowsReservedName(s) {
		return "", errors.New("not found")
	}
	return filepath.Join(resolvePath(baseDir), s), nil
}

func windowsReservedName(name string) bool {
	stem := strings.ToUpper(name)
	if i := strings.Index(stem, "."); i >= 0 {
		stem = stem[:i]
	}
	stem = strings.TrimRight(stem, " ")
	switch stem {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return true
	}
	if len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '1' && stem[3] <= '9' {
		return true
	}
	return false
}

func (l *Library) registerAppRuntime(b *Bridge) {
	b.handlers["app:getRuntimePaths"] = func(context.Context, []json.RawMessage) (any, error) {
		p := l.paths
		return map[string]any{
			"userDataPath": p.UserData, "dataDir": p.DataDir(), "databasePath": p.DatabasePath(),
			"backupsDir": p.BackupsDir(), "logsDir": p.LogsDir(),
		}, nil
	}
	b.handlers["app:relaunch"] = func(context.Context, []json.RawMessage) (any, error) {
		if l.relaunch == nil {
			return nil, errors.New("重启不可用")
		}
		// 先把 {success:true} 回给渲染进程，再重启
		go func() { time.Sleep(200 * time.Millisecond); l.relaunch() }()
		return map[string]any{"success": true}, nil
	}
	b.sends["log:appError"] = func(a []json.RawMessage) {
		entry := jsArg(a, 0)
		if !jsIsObject(entry) {
			return
		}
		message := readLogField(jsGet(entry, "message"), 2000)
		if message == "" {
			return
		}
		scope := readLogField(jsGet(entry, "scope"), 64)
		if scope == "" {
			scope = "renderer"
		}
		action := readLogField(jsGet(entry, "action"), 64)
		if action == "" {
			action = "unknown"
		}
		l.paths.logAppError(scope, action, message, nil)
	}

	b.handlers["dialog:selectImage"] = func(context.Context, []json.RawMessage) (any, error) {
		if l.openDialog == nil {
			return nil, errors.New("文件选择对话框不可用")
		}
		paths, err := l.openDialog(true, "Images", []string{"jpg", "png", "gif", "jpeg", "webp"})
		if err != nil {
			return nil, err
		}
		if len(paths) > 0 {
			l.images.set(paths)
			return paths, nil
		}
		l.images.set(nil)
		return []string{}, nil
	}
	b.handlers["image:save"] = func(_ context.Context, a []json.RawMessage) (any, error) {
		imagesDir := l.paths.ImagesDir()
		if err := os.MkdirAll(imagesDir, 0o755); err != nil {
			return nil, err
		}
		var list jsArr
		switch x := jsArg(a, 0).(type) {
		case jsArr:
			list = x
		case string:
			// 字符串可迭代：逐字符当作路径，不会命中选择集，结果为空，但选择集照常清空
		default:
			return nil, errors.New("filePaths is not iterable")
		}
		saved := []string{}
		for _, v := range list {
			fp, ok := v.(string)
			if !ok {
				continue
			}
			resolved := resolvePath(fp)
			if !l.images.has(resolved) {
				continue // Image path was not selected through the file picker
			}
			ext := nodeExtname(fp)
			if !imageExtensions[strings.ToLower(ext)] {
				continue
			}
			name := newUUID() + ext
			data, err := os.ReadFile(resolved)
			if err != nil {
				continue
			}
			if err := os.WriteFile(filepath.Join(imagesDir, name), data, 0o644); err != nil {
				continue
			}
			saved = append(saved, name)
		}
		l.images.set(nil)
		return saved, nil
	}
	b.handlers["image:readBase64"] = func(_ context.Context, a []json.RawMessage) (any, error) {
		full, err := validateImageFileName(jsArg(a, 0), l.paths.ImagesDir())
		if err != nil {
			return nil, nil
		}
		data, err := os.ReadFile(full)
		if err != nil {
			return nil, nil
		}
		return base64.StdEncoding.EncodeToString(data), nil
	}
}
