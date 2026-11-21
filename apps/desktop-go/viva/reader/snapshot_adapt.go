package reader

import (
	"net/url"
	"regexp"
	"strings"
)

// 网页快照整页 HTML（services.LibraryWebSnapshotView.Document，jsrc 的 snapshotDocument 生成）放进阅读岛的沙箱 iframe（srcdoc）之前的适配。
//
// 那份文档原本是给渲染进程的沙箱 iframe 写的：它自带一条 <meta> 内容安全策略（脚本只许桥接脚本的 sha256，图片只许 `local-image:`），
// 文章里的图片地址是 `local-image://<名称>`。在阅读岛里这两处都不成立：
//
//   - 岛里没有 `local-image:` 这个 scheme，本地图片由宿主在岛的自有地址（页面是 https://gzreader.viva-app.invalid/doc/index.html，
//     图片在 ./media/local-image/<名称>，serve.go，名称与扩展名由 Media 校验）提供，页面里统一用相对文档的写法（srcdoc 文档的基准地址继承自岛页面）。
//     但沙箱 iframe 没有 allow-same-origin，来源是不透明的，在真 WebView2 里它的请求到不了岛的自有地址（readersmoke 的 snapshot 场景实测，
//     宿主的媒体回调一次都没被调用；换成 allow-same-origin 就能取到，但那是台账 A-018 的红线）。所以真正的读图在页面一侧：
//     可信的主框架按这里改写出的相对地址取回图片、内联成 data: 地址再交给 iframe（renderer/reader/snapshot-images.ts）；
//   - srcdoc 文档继承岛页面的内容安全策略，与它自带的 <meta> 策略取交集：岛页面的 `script-src 'self'` 会挡掉内联的桥接脚本，
//     所以岛的策略里放行桥接脚本的那一个精确 sha256（SnapshotBridgeSHA256，见 serve.go），不放 'unsafe-inline'，也不放开任何外部域；
//     <meta> 策略里的 `img-src local-image:` 则换成 `img-src data:`（页面内联进来的图片），文档里的外部图片因此仍被它挡掉。
//
// 适配只做这两件事：换 <meta> 里的图片来源、改写 `local-image://` 地址；文档其余内容（含净化结果与桥接脚本）逐字不动。

// SnapshotBridgeSHA256 是快照桥接脚本（jsrc/services/web-capture/snapshot-bridge.ts 的 SNAPSHOT_BRIDGE）的 sha256，base64 编码。
// 这是岛放行内联脚本的唯一例外：与桥接脚本逐字绑定，脚本有任何改动哈希就变，岛会拒绝运行它。
// 与真实文档的对应由主包的 TestSnapshotBridgeHashMatchesTheEngine 守住（脚本改了而这里没跟着改，测试失败）。
const SnapshotBridgeSHA256 = "r/j/iP5bF1QOOiOFZe2lCRqOSsNlk7RA4jLov0Y8zg0="

const (
	// snapshotMetaImg 是文档 <meta> 策略里的图片来源（snapshotDocument 带实例标识时的写法）；snapshotMetaImgIsland 是岛里的写法：
	// 只许 data:（本地图片由页面读出后内联进来），仍然不放开任何外部来源。
	snapshotMetaImg       = "img-src local-image:;"
	snapshotMetaImgIsland = "img-src data:;"

	localImagePrefix = "local-image://"
	// islandImageDir 是岛里本地图片的相对路径前缀（相对文档，不能写成根相对的 /media/…，见 serve.go 的 serve）。
	islandImageDir = "./media/local-image/"
)

// localImageRef 匹配文档里的一处 `local-image://<名称>`：名称到引号、括号、空白、尖括号、& 与反斜杠为止
// （属性值、CSS url() 与正文里的出现方式都落在这个范围内；净化器只放行资源清单里的文件名，见 snapshot-sanitize.ts）。
var localImageRef = regexp.MustCompile(`local-image://[^"'()\s<>&\\]+`)

// AdaptSnapshotDocument 把快照文档适配到阅读岛的 srcdoc（见本文件开头）。名称里有 `.` 或 `..` 段的地址不改写（页面里显示成裂图），
// 不让它借相对路径跳出 media/local-image/；其余名称按「解码再逐段编码」规整。
func AdaptSnapshotDocument(doc string) string {
	doc = strings.Replace(doc, snapshotMetaImg, snapshotMetaImgIsland, 1)
	if !strings.Contains(doc, localImagePrefix) {
		return doc
	}
	return localImageRef.ReplaceAllStringFunc(doc, func(ref string) string {
		if rewritten, ok := islandImageURL(ref); ok {
			return rewritten
		}
		return ref
	})
}

// islandImageURL 把 `local-image://<名称>` 换成岛里的相对地址；名称为空或含 `.`、`..` 段时 ok 为假。
func islandImageURL(ref string) (string, bool) {
	rest := strings.TrimPrefix(ref, localImagePrefix)
	if decoded, err := url.PathUnescape(rest); err == nil {
		rest = decoded
	}
	var segments []string
	for _, segment := range strings.Split(rest, "/") {
		switch segment {
		case "":
			continue
		case ".", "..":
			return "", false
		}
		segments = append(segments, url.PathEscape(segment))
	}
	if len(segments) == 0 {
		return "", false
	}
	return islandImageDir + strings.Join(segments, "/"), true
}
