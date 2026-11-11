//go:build !patched

package extract

import readability "codeberg.org/readeck/go-readability/v2"

// 默认构建不改动库的行为；产品模式的结果需要 -tags patched。
func applyProductOptions(p *readability.Parser) {}
