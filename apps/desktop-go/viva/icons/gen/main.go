// Command gen 把一个目录里的 SVG 编译成 gfx.Icon 的 Go 源文件，写到标准输出（-o 指定文件则写文件）。
//
// 由 apps/desktop-go/scripts/gen-native-icons.mjs 调用：脚本把渲染层用到的 lucide 图标写成临时目录里的
// SVG，再运行本工具得到 viva/icons/icons_gen.go。编译器是 Viva 自带的 tools/iconcompile
// （与 `viva icons` 命令同一个实现），所以生成物与 Viva 的图标绘制路径一一对应；运行期没有 SVG 解析。
//
// Command gen compiles a directory of SVG files into Go source of gfx.Icon values.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/tools/iconcompile"
)

const defaultLicense = "Lucide icons (https://lucide.dev), ISC License; compiled from the lucide-react package the renderer imports."

func main() {
	out := flag.String("o", "-", "输出文件；- 表示标准输出")
	pkg := flag.String("pkg", "icons", "生成文件的包名")
	license := flag.String("license", defaultLicense, "写进生成文件头部的版权说明（可多行）")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "用法：gen [-o 文件] [-pkg 包名] [-license 说明] <SVG 目录>")
		os.Exit(2)
	}
	if err := run(flag.Arg(0), *out, *pkg, *license); err != nil {
		fmt.Fprintln(os.Stderr, "viva/icons/gen:", err)
		os.Exit(1)
	}
}

func run(dir, out, pkg, license string) error {
	files, err := filepath.Glob(filepath.Join(dir, "*.svg"))
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("%s 里没有 SVG 文件", dir)
	}
	sort.Strings(files)
	icons := make(map[string]gfx.Icon, len(files))
	for _, file := range files {
		source, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		name := strings.TrimSuffix(filepath.Base(file), ".svg")
		// Viva 的编译器把半径不等的椭圆弧的控制点算错了（台账 B-005），编译前先改写成三次曲线；没有椭圆弧时原样不动。
		source, err = expandEllipticalArcs(source)
		if err != nil {
			return fmt.Errorf("展开 %s 的椭圆弧：%w", name, err)
		}
		icon, err := iconcompile.Compile(name, source)
		if err != nil {
			return fmt.Errorf("编译 %s：%w", name, err)
		}
		icons[name] = icon
	}
	src, err := iconcompile.Generate(pkg, icons, license)
	if err != nil {
		return err
	}
	if out == "-" {
		_, err = os.Stdout.Write(src)
		return err
	}
	return os.WriteFile(out, src, 0o644)
}
