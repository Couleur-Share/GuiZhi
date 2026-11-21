package reader

import (
	"embed"
	"path"
	"strconv"
	"strings"
)

// assets 是阅读页面的构建产物（apps/desktop-go/jsgen/build-reader.mts 生成，入库；CI 要求与重新生成的结果一致）。
//
//go:embed assets
var assets embed.FS

// scheme 是岛的自有 scheme：页面地址 gzreader://doc/index.html。与默认的 gzisland 区分开，日志与排查时一眼看出是阅读岛。
const scheme = "gzreader"

// cspRest 是阅读岛与图片查看岛共用的策略（script-src 之外的部分）：以 island.DefaultCSP 为底，只放宽图片——文章里的远程图片与渲染层一样直接加载
// （渲染层的阅读区同样是 <img src="https://…">），样式、字体、媒体、连接仍然只许自有 scheme。
const cspRest = "style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob: https: http:; font-src 'self' data:; media-src 'self' data: blob:; " +
	"connect-src 'self'; frame-src about:; object-src 'none'; base-uri 'none'; form-action 'none'"

// viewerCSP 是图片查看岛的内容安全策略：脚本只许自有 scheme 的文件。
const viewerCSP = "default-src 'none'; script-src 'self'; " + cspRest

// readerCSP 是阅读岛的内容安全策略：脚本许自有 scheme 的文件，外加网页快照桥接脚本的那一个精确 sha256（SnapshotBridgeSHA256）。
// 快照文档放在沙箱 iframe 的 srcdoc 里，继承这份策略；桥接脚本是文档里唯一的内联脚本，只有内容与哈希逐字相符才会运行，
// 文章里夹带的任何别的内联脚本、事件属性、外部脚本都被拦下（readersmoke 的 snapshot 场景在真 WebView2 里实测）。
// 没有放开 'unsafe-inline' 脚本，也没有放开任何外部域。
const readerCSP = "default-src 'none'; script-src 'self' 'sha256-" + SnapshotBridgeSHA256 + "'; " + cspRest

const htmlTag = `<html lang="zh-CN">`

// 默认外观：与渲染层 globals.css 里 :root 的默认值一致（浅色、色相 210、饱和度 35、根字号 16 px）。
var defaultTheme = Theme{Hue: 210, Saturation: 35, BasePx: 16, Motion: MotionStandard}

// serve 是 island.Config.Serve：按路径提供页面文件与本地媒体。路径不含主机名与查询串，没有前导斜杠（兼容有的情形）。
// Viva 把页面地址的第一段当主机段（页面是 doc/index.html，主机段 doc），宿主只收到主机段之后的路径：
// 所以页面里的媒体地址必须相对文档（./media/…，解析成 doc/media/…，这里收到 media/…），写成根相对的 /media/… 就会被拆成主机 media。
func (h *Host) serve(p string) (mime string, data []byte, ok bool) {
	p = strings.TrimPrefix(p, "/")
	switch {
	case p == "" || p == "index.html":
		raw, err := assets.ReadFile("assets/index.html")
		if err != nil {
			return "", nil, false
		}
		return "text/html; charset=utf-8", bakeHTML(raw, h.req.Theme, h.req.Lang), true
	case strings.HasPrefix(p, "media/"):
		return serveMedia(h.opts.Media, strings.TrimPrefix(p, "media/"))
	}
	return serveAssetIn("assets/", p)
}

// serveAssetIn 提供嵌入目录 dir（以斜杠结尾，阅读岛是 assets/，查看岛是 assets/viewer/）下的静态文件：
// 只认规整的相对路径（没有 ..、反斜杠、冒号、空字节）。
func serveAssetIn(dir, p string) (string, []byte, bool) {
	if strings.ContainsAny(p, "\\:\x00") || path.Clean(p) != p || strings.HasPrefix(p, ".") {
		return "", nil, false
	}
	data, err := assets.ReadFile(dir + p)
	if err != nil {
		return "", nil, false
	}
	return assetMIME(p), data, true
}

func assetMIME(p string) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".html":
		return "text/html; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".woff2":
		return "font/woff2"
	case ".woff":
		return "font/woff"
	case ".png":
		return "image/png"
	}
	return "application/octet-stream"
}

// serveMedia 提供 media/<scheme>/<名称>：scheme 只认 local-image 与 local-video，名称交给 Media 校验。阅读岛与查看岛共用。
func serveMedia(media Media, rest string) (string, []byte, bool) {
	if media == nil {
		return "", nil, false
	}
	kind, name, found := strings.Cut(rest, "/")
	if !found || name == "" || (kind != "local-image" && kind != "local-video") {
		return "", nil, false
	}
	return media(kind, name)
}

// bakeHTML 把外观烘进 index.html 的 <html> 标签：页面脚本执行之前样式表就已经按当前主题算出令牌，
// 打开岛时不会先闪一下默认的浅色背景再换成深色（脚本随后的 applyTheme 写入同样的值，不产生变化）。
func bakeHTML(raw []byte, theme Theme, lang Lang) []byte {
	if theme.Validate() != nil {
		theme = defaultTheme
	}
	var tag strings.Builder
	tag.WriteString(`<html lang="`)
	if lang == LangEn {
		tag.WriteString("en")
	} else {
		tag.WriteString("zh-CN")
	}
	tag.WriteString(`"`)
	if theme.Dark {
		tag.WriteString(` class="dark"`)
	}
	tag.WriteString(` style="--theme-hue:` + formatNumber(theme.Hue) + `;--theme-saturation:` + formatNumber(theme.Saturation) +
		`;--base-font-size:` + formatNumber(theme.BasePx) + `px"`)
	tag.WriteString(` data-motion="` + string(theme.Motion) + `">`)
	return []byte(strings.Replace(string(raw), htmlTag, tag.String(), 1))
}

// formatNumber 与 JavaScript 的 String(number) 在这里用到的数值范围内一致：整数不带小数点，小数取最短表示。
func formatNumber(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
