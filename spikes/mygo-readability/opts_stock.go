// 默认构建不改动库的任何行为（stock 模式）。

//go:build !patched

package main

import readability "codeberg.org/readeck/go-readability/v2"

func applyProductOptions(p *readability.Parser) {}
