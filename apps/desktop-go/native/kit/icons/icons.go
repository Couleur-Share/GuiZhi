// Package icons 提供原生界面用的 Lucide 图标：SVG 源由 gen-native-icons.mjs 生成（icons_gen.go），
// 首次使用时才解析并缓存，没用到的图标不占内存。
//
// Package icons serves the Lucide icons of the native UI. The SVG sources are generated; each icon is
// parsed lazily on first use and cached.
package icons

import (
	"log"
	"sync"

	"github.com/egoist/mygo/ui"
)

// Name 是图标的 canonical 名（Search、CircleAlert…），常量见 icons_gen.go。
type Name string

var (
	mu     sync.Mutex
	parsed = map[Name]*ui.SVG{}
)

// Get 返回图标的 SVG；未知名字或解析失败返回 nil（ui.Icon 对 nil 什么也不画）并记一条日志。
func Get(n Name) *ui.SVG {
	mu.Lock()
	defer mu.Unlock()
	if svg, ok := parsed[n]; ok {
		return svg
	}
	src, ok := sources[n]
	if !ok {
		log.Printf("[icons] 未知图标 %q（渲染层没有用到它？重新运行 gen-native-icons.mjs）", n)
		parsed[n] = nil
		return nil
	}
	svg, err := ui.ParseSVG([]byte(src))
	if err != nil {
		log.Printf("[icons] 解析图标 %q 失败：%v", n, err)
		svg = nil
	}
	parsed[n] = svg
	return svg
}

// Names 返回全部可用图标名（测试与对账用）。
func Names() []Name {
	out := make([]Name, 0, len(sources))
	for n := range sources {
		out = append(out, n)
	}
	return out
}
