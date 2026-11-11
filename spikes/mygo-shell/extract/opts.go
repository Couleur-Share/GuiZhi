package extract

import readability "codeberg.org/readeck/go-readability/v2"

// 产品对 Readability 的两处改动（见 third_party/readability/FORK.md）。
func applyProductOptions(p *readability.Parser) {
	p.LinkDensityModifier = 0.1
	p.KeepTitleHeader = true
}
