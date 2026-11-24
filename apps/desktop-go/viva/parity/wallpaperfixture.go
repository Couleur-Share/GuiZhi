package parity

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	_ "image/png" // 注册 PNG 解码器，读夹具尺寸用
)

// WallpaperFixtureName 是壁纸夹具落在图片目录里的文件名：React 采集器把同一个文件拷进会话的 <userData>/data/assets/images/
// （scripts/parity/lib/react-wallpaper.mjs），设置里的 backgroundImageFileName 就是它；Viva 场景的读图回调按这个名字返回字节。
const WallpaperFixtureName = "wallpaper-fixture.png"

// wallpaperFixturePNG 是确定性的合成壁纸（640×360，由 scripts/parity/tools/make-wallpaper-fixture.mjs 生成）：
// 四块对角平色带平滑过渡加软边圆与软边圆角矩形（不含硬边，见 G-038），同时检验居中裁剪、不透明度混合与高斯模糊。
//
//go:embed testdata/wallpaper-fixture.png
var wallpaperFixturePNG []byte

// WallpaperFixture 返回壁纸夹具的字节（拷贝，调用方可随意改动）。
func WallpaperFixture() []byte { return bytes.Clone(wallpaperFixturePNG) }

// WallpaperFixtureSize 是夹具的像素尺寸；夹具是编译进来的常量，解码失败是程序错误。
func WallpaperFixtureSize() (width, height int) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(wallpaperFixturePNG))
	if err != nil {
		panic(fmt.Errorf("解码壁纸夹具失败：%w", err))
	}
	return cfg.Width, cfg.Height
}
