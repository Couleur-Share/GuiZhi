//go:build patched

package extract

import readability "codeberg.org/readeck/go-readability/v2"

// 产品对 Readability 的两处改动（见 prepare-patched.ps1）。
func applyProductOptions(p *readability.Parser) {
	p.LinkDensityModifier = 0.1
	p.KeepTitleHeader = true
}
