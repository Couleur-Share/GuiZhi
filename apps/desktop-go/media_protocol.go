package main

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/egoist/mygo"
)

// 本地媒体协议：渲染进程用 local-image:// / local-video:// 引用用户数据目录里的素材。
// Electron 版靠 protocol.registerFileProtocol；MyGo 里改为 Protocol.HandleFunc，
// WebView2 实际服务在 http://<scheme>.localhost/，由 preload 的 shim/media-urls.ts 把页面里的
// 自定义协议地址改写过去。路径校验照搬 main/local-media-protocol.ts。
// Local media protocols: the renderer references files in the user data dir via
// local-image:// and local-video://; served from Go, validated like the Electron version.

var (
	localImageExts = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true}
	localVideoExts = map[string]bool{".mp4": true, ".webm": true, ".mov": true, ".avi": true, ".mkv": true}
)

// 与 local-media-protocol.ts 的 UNSAFE_SEGMENT_PATTERN 一致：控制字符与冒号（挡盘符、ADS 流名）。
var unsafeMediaSegment = regexp.MustCompile("[\x00-\x1f\x7f:]")

var mediaPathSplit = regexp.MustCompile(`[\\/]+`)

// resolveLocalMediaPath 把请求路径解析为 baseDir 内的绝对路径；任何一项不满足都返回 false。
func resolveLocalMediaPath(baseDir, rawPath string, allowedExts map[string]bool) (string, bool) {
	if strings.HasPrefix(rawPath, "/") || strings.HasPrefix(rawPath, `\`) {
		return "", false
	}
	var segments []string
	for _, s := range mediaPathSplit.Split(rawPath, -1) {
		if s != "" {
			segments = append(segments, s)
		}
	}
	if len(segments) == 0 {
		return "", false
	}
	for _, s := range segments {
		if s == "." || s == ".." || unsafeMediaSegment.MatchString(s) {
			return "", false
		}
	}
	if !allowedExts[strings.ToLower(filepath.Ext(segments[len(segments)-1]))] {
		return "", false
	}
	base, err := filepath.Abs(baseDir)
	if err != nil {
		return "", false
	}
	full := filepath.Join(append([]string{base}, segments...)...)
	rel, err := filepath.Rel(base, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return full, true
}

// localMediaHandler 服务 baseDir 下的媒体文件。请求 URL 形如 local-image://localhost/<名称>，
// 名称段已被 URL 解码（r.URL.Path）。ServeContent 提供 Range（视频拖动进度条需要）与 304。
func localMediaHandler(baseDir string, allowedExts map[string]bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// URL 里的第一个 "/" 是路径分隔符，不属于文件名；其后若再以 / 或 \ 开头视为绝对路径，由解析函数拒绝。
		p := strings.TrimPrefix(r.URL.Path, "/")
		full, ok := resolveLocalMediaPath(baseDir, p, allowedExts)
		if !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		f, err := os.Open(full)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil || st.IsDir() {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, st.Name(), st.ModTime(), f)
	})
}

// registerLocalMediaProtocols 必须在创建窗口之前调用（窗口按已注册协议建立拦截过滤器）。
func registerLocalMediaProtocols(paths Paths) error {
	if err := mygo.Protocol.Handle("local-image", localMediaHandler(paths.ImagesDir(), localImageExts)); err != nil {
		return err
	}
	return mygo.Protocol.Handle("local-video", localMediaHandler(paths.VideosDir(), localVideoExts))
}
