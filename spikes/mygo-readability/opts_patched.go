// 产品模式的 Readability 改动：只在 -tags patched 时生效（需要先运行 prepare-patched.ps1 得到打过补丁的库副本）。

//go:build patched

package main

import readability "codeberg.org/readeck/go-readability/v2"

func applyProductOptions(p *readability.Parser) {
	p.LinkDensityModifier = 0.1
	p.KeepTitleHeader = true
}
