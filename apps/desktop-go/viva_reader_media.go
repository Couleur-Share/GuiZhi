package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"guizhi-mygo-shell/viva/reader"
)

// Viva 阅读岛的本地媒体：文章里的 local-image://<名称> 在岛里被页面改写成 ./media/local-image/<名称>（相对页面文档，解析后宿主收到 media/local-image/<名称>），由宿主回调到这里取字节。
// 路径校验与 media_protocol.go 同一套（resolveLocalMediaPath：只认用户数据目录 images 下、扩展名在白名单内的相对路径）；
// 这里再加两条岛特有的：只给图片（视频不整个读进内存），单张超过上限不给——页面里显示成裂图，与文件缺失同一种表现。

// readerMediaMaxBytes 是阅读岛一次取用的单张本地图片的上限。
const readerMediaMaxBytes = 32 << 20

// readerImageMIME 是白名单扩展名到内容类型的对应；不依赖系统注册表（那里 .webp 的登记因机器而异）。
var readerImageMIME = map[string]string{
	".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".gif": "image/gif", ".webp": "image/webp",
}

// readerMedia 是接到 vivaapp.Options.ReaderMedia 上的实现。
func readerMedia(paths Paths) reader.Media {
	return readerMediaFromDir(paths.ImagesDir(), readerMediaMaxBytes)
}

// readerMediaFromDir 在 imagesDir 下取图片；任何一项校验不过都返回 false。
func readerMediaFromDir(imagesDir string, maxBytes int64) reader.Media {
	return func(scheme, name string) (mime string, data []byte, ok bool) {
		if scheme != "local-image" {
			return "", nil, false
		}
		full, ok := resolveLocalMediaPath(imagesDir, name, localImageExts)
		if !ok {
			return "", nil, false
		}
		f, err := os.Open(full)
		if err != nil {
			return "", nil, false
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil || !st.Mode().IsRegular() || st.Size() > maxBytes {
			return "", nil, false
		}
		// 多读一个字节：文件在 Stat 之后变大时也不会越过上限。
		data, err = io.ReadAll(io.LimitReader(f, maxBytes+1))
		if err != nil || int64(len(data)) > maxBytes {
			return "", nil, false
		}
		return readerImageMIME[strings.ToLower(filepath.Ext(full))], data, true
	}
}
