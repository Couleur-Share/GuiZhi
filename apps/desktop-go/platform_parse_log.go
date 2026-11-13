package main

// 平台页面结构异常时写诊断日志（不写完整 HTML）。对应 import/platform-parse-log.ts。

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

func (l *Library) logPlatformStructureMissing(platform, marker, html, action string) {
	sum := sha256.Sum256([]byte(html))
	extra := newObj()
	extra.set("code", "structure_missing")
	extra.set("platform", platform)
	extra.set("marker", marker)
	extra.set("markerPresent", strings.Contains(html, marker))
	extra.set("htmlLength", float64(utf16Len(html)))
	extra.set("htmlHash8", hex.EncodeToString(sum[:])[:8])
	l.paths.logAppError("import", action, "[structure_missing] "+platform+" 页面结构异常", extra)
}
