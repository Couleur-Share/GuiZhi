package main

// 托管工具（yt-dlp / ffmpeg / FunASR）共用部分，对应：
//   - services/picked-binary-paths.ts   文件选择器返回过的可执行文件路径登记表
//   - services/media/engine-status-cache.ts   状态探测缓存（键 + TTL，合并并发探测）
//   - services/media/engine-version-store.ts  版本号跨启动缓存（tools/engine-versions.json）
//   - services/media/tool-download.ts   流式下载、校验清单解析、SHA-256

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	whatwg "github.com/nlnwa/whatwg-url/url"
)

// ── 文件选择器登记表 ──
//
// ytDlpPath / ffmpegPath 最终会被主进程 spawn；渲染进程不能往 settings 里写任意路径，
// 所以这两个键只接受本会话里文件选择器返回过的路径（已保存的旧值不受影响）。

var pickedBinary = struct {
	sync.Mutex
	paths map[string]bool
}{paths: map[string]bool{}}

func rememberPickedBinaryPath(p string) {
	if strings.TrimFunc(p, jsSpace) == "" {
		return
	}
	pickedBinary.Lock()
	pickedBinary.paths[p] = true
	pickedBinary.Unlock()
}

// isPickedBinaryPath 对应 isAcceptableBinaryPath 的非空分支：空串由调用方放行。
func isPickedBinaryPath(p string) bool {
	pickedBinary.Lock()
	defer pickedBinary.Unlock()
	return pickedBinary.paths[p]
}

// ── 状态缓存 ──

const engineStatusTTL = 5 * time.Minute

// statusCache 按「配置键 + TTL」缓存探测结果，并合并同键的并发探测。
type statusCache[T any] struct {
	mu      sync.Mutex
	ttl     time.Duration
	now     func() time.Time
	entry   *statusEntry[T]
	pending *statusPending[T]
}

type statusEntry[T any] struct {
	key   string
	value T
	at    time.Time
}

type statusPending[T any] struct {
	key  string
	done chan struct{}
	val  T
	err  error
}

func newStatusCache[T any]() *statusCache[T] {
	return &statusCache[T]{ttl: engineStatusTTL, now: time.Now}
}

func (c *statusCache[T]) read(key string, load func() (T, error), force bool) (T, error) {
	c.mu.Lock()
	if !force && c.entry != nil && c.entry.key == key && c.now().Sub(c.entry.at) < c.ttl {
		v := c.entry.value
		c.mu.Unlock()
		return v, nil
	}
	if p := c.pending; p != nil && p.key == key {
		c.mu.Unlock()
		<-p.done
		return p.val, p.err
	}
	p := &statusPending[T]{key: key, done: make(chan struct{})}
	c.pending = p
	c.mu.Unlock()

	p.val, p.err = load()
	c.mu.Lock()
	if p.err == nil {
		c.entry = &statusEntry[T]{key: key, value: p.val, at: c.now()}
	}
	if c.pending == p {
		c.pending = nil
	}
	c.mu.Unlock()
	close(p.done)
	return p.val, p.err
}

func (c *statusCache[T]) invalidate() {
	c.mu.Lock()
	c.entry = nil
	c.mu.Unlock()
}

// ── 版本号跨启动缓存 ──

type versionProbe func(ctx context.Context, executable string) (string, bool)

type cachedVersion struct {
	Size    int64   `json:"size"`
	MtimeMs float64 `json:"mtimeMs"`
	Version string  `json:"version"`
}

var engineVersionMu sync.Mutex

func (l *Library) engineVersionCachePath() string {
	return filepath.Join(l.paths.ToolsDir(), "engine-versions.json")
}

// resolveFromSearchPath 把裸命令名按 PATH + PATHEXT 解析成真实文件；找不到返回空串。
func resolveFromSearchPath(command string) string {
	exts := []string{""}
	if isWindows() {
		pe := os.Getenv("PATHEXT")
		if pe == "" {
			pe = ".EXE"
		}
		for _, e := range strings.Split(pe, ";") {
			if e != "" {
				exts = append(exts, e)
			}
		}
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		for _, e := range exts {
			c := filepath.Join(dir, command+e)
			if _, err := os.Stat(c); err == nil {
				return c
			}
		}
	}
	return ""
}

type executableIdentity struct {
	file    string
	size    int64
	mtimeMs float64
}

func identifyExecutable(executable string) (executableIdentity, bool) {
	file := executable
	if !strings.ContainsAny(executable, `\/`) {
		if file = resolveFromSearchPath(executable); file == "" {
			return executableIdentity{}, false
		}
	}
	st, err := os.Stat(file)
	if err != nil || !st.Mode().IsRegular() {
		return executableIdentity{}, false
	}
	abs, err := filepath.Abs(file)
	if err != nil {
		return executableIdentity{}, false
	}
	return executableIdentity{file: abs, size: st.Size(), mtimeMs: float64(st.ModTime().UnixNano()) / 1e6}, true
}

func (l *Library) readEngineVersionCache() map[string]cachedVersion {
	out := map[string]cachedVersion{}
	raw, err := os.ReadFile(l.engineVersionCachePath())
	if err != nil {
		return out
	}
	var parsed map[string]cachedVersion
	if json.Unmarshal(raw, &parsed) != nil || parsed == nil {
		return out
	}
	return parsed
}

// writeEngineVersionCache 顺手丢掉指向已消失文件的条目。
func (l *Library) writeEngineVersionCache(cache map[string]cachedVersion) {
	alive := map[string]cachedVersion{}
	for f, v := range cache {
		if _, err := os.Stat(f); err == nil {
			alive[f] = v
		}
	}
	if err := os.MkdirAll(filepath.Dir(l.engineVersionCachePath()), 0o755); err != nil {
		return
	}
	b, err := json.MarshalIndent(alive, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(l.engineVersionCachePath(), append(b, '\n'), 0o644); err != nil {
		logAppErrorf("main", "引擎版本缓存", "版本缓存写入失败（不影响探测结果）：%v", err)
	}
}

// withCachedVersion 给版本探测包一层持久化缓存：同一文件只在首次（或 force）时真去 spawn；只缓存成功结果。
func (l *Library) withCachedVersion(probe versionProbe, force bool) versionProbe {
	return func(ctx context.Context, executable string) (string, bool) {
		id, ok := identifyExecutable(executable)
		if !ok {
			return probe(ctx, executable)
		}
		if !force {
			engineVersionMu.Lock()
			c, hit := l.readEngineVersionCache()[id.file]
			engineVersionMu.Unlock()
			if hit && c.Size == id.size && c.MtimeMs == id.mtimeMs {
				return c.Version, true
			}
		}
		version, ok := probe(ctx, executable)
		if ok {
			engineVersionMu.Lock()
			cache := l.readEngineVersionCache()
			cache[id.file] = cachedVersion{Size: id.size, MtimeMs: id.mtimeMs, Version: version}
			l.writeEngineVersionCache(cache)
			engineVersionMu.Unlock()
		}
		return version, ok
	}
}

// ── 版本探测（spawn） ──

const versionProbeTimeout = 10 * time.Second

// probeExecutable 运行 `<exe> <arg>`，退出码 0 且 parse 得出版本才算可用；失败/超时/不存在均返回 false。
func probeExecutable(ctx context.Context, executable, arg string, parse func(stdout string) string) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, versionProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, arg)
	hideChildWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	v := parse(string(out))
	return v, v != ""
}

// ── 下载 ──

// toolHTTPClient 返回托管工具下载用的客户端：按网络代理设置选路，超时由调用方的 context 控制。
func (l *Library) toolHTTPClient(noRedirect bool) *http.Client {
	if l.toolsHTTP != nil {
		c := *l.toolsHTTP
		if noRedirect {
			c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		}
		return &c
	}
	c := &http.Client{Transport: newProxiedTransport()}
	if noRedirect {
		c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	return c
}

type toolDownloadProgress struct {
	Transferred int64  `json:"transferred"`
	Total       *int64 `json:"total"`
}

const (
	toolDownloadTimeout   = 5 * time.Minute
	progressEmitInterval  = 200 * time.Millisecond
	checksumFetchTimeout  = 30 * time.Second
	headRequestTimeout    = 8 * time.Second
	releaseRequestTimeout = 6 * time.Second
)

// downloadToFile 对应 downloadToFile：流式写盘，节流回调进度。
func (l *Library) downloadToFile(ctx context.Context, url, target string, onProgress func(toolDownloadProgress)) error {
	ctx, cancel := context.WithTimeout(ctx, toolDownloadTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := l.toolHTTPClient(false).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var total *int64
	if n := resp.ContentLength; n > 0 {
		total = &n
	}
	f, err := os.Create(target)
	if err != nil {
		return err
	}
	var transferred int64
	var last time.Time
	buf := make([]byte, 64*1024)
	emit := func() {
		if onProgress != nil {
			onProgress(toolDownloadProgress{Transferred: transferred, Total: total})
		}
	}
	var copyErr error
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				copyErr = werr
				break
			}
			transferred += int64(n)
			if time.Since(last) >= progressEmitInterval {
				last = time.Now()
				emit()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			copyErr = rerr
			break
		}
	}
	if cerr := f.Close(); copyErr == nil {
		copyErr = cerr
	}
	if copyErr != nil {
		return copyErr
	}
	emit()
	return nil
}

var reSha256Line = regexp.MustCompile(`(?i)^([0-9a-f]{64})\s+\*?(.+)$`)

// parseSha256Sums 取出 SHA2-256SUMS 风格清单里某个文件的哈希（小写）；没有返回空串。
func parseSha256Sums(text, fileName string) string {
	for _, raw := range regexp.MustCompile(`\r?\n`).Split(text, -1) {
		if m := reSha256Line.FindStringSubmatch(strings.TrimFunc(raw, jsSpace)); m != nil && strings.TrimFunc(m[2], jsSpace) == fileName {
			return strings.ToLower(m[1])
		}
	}
	return ""
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// fetchExpectedSha256 按镜像顺序取校验清单；全部取不到返回空串（是否放行由调用方决定）。
func (l *Library) fetchExpectedSha256(ctx context.Context, urls []string, fileName string) string {
	for _, u := range urls {
		if v := l.fetchChecksumFrom(ctx, u, fileName); v != "" {
			return v
		}
	}
	return ""
}

func (l *Library) fetchChecksumFrom(ctx context.Context, url, fileName string) string {
	ctx, cancel := context.WithTimeout(ctx, checksumFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ""
	}
	resp, err := l.toolHTTPClient(false).Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return ""
	}
	return parseSha256Sums(string(body), fileName)
}

// toolInstallFailure 汇总各下载源的失败原因（与 TS 的 `所有下载源均失败——…` 一致）。
func toolInstallFailure(failures []string) error {
	return errors.New("所有下载源均失败——" + strings.Join(failures, "；"))
}

func hostOf(rawURL string) string {
	if u, err := whatwg.Parse(rawURL); err == nil {
		return u.Hostname()
	}
	return rawURL
}

func isWindows() bool { return runtime.GOOS == "windows" }
