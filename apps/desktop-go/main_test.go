package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// frozenGoldenDirs：环境变量 -> testdata/golden 下冻结的对拍基准子目录。
// 基准由 Electron 终版（标签 electron-final-0.28.x）的 TS 实现生成，来源与重新生成方式见 testdata/golden/README.md。
// 显式设置了环境变量时以环境变量为准（便于用新生成的基准做对比）；未设置时默认读冻结目录，
// 这样 CI 与本地的 go test 默认就会跑对拍，而不是静默跳过。
var frozenGoldenDirs = map[string]string{
	"GUIZHI_GOLDEN_DIR":              "main",
	"GUIZHI_FORUM_GOLDEN_DIR":        "forum",
	"GUIZHI_MEDIA_GOLDEN_DIR":        "media",
	"GUIZHI_THEMED_GOLDEN_DIR":       "themed",
	"GUIZHI_THEMED_DB_GOLDEN_DIR":    "themed-db",
	"GUIZHI_THEMED_IPC_GOLDEN_DIR":   "themed-ipc",
	"GUIZHI_THEMED_PIPE_GOLDEN_DIR":  "themed-pipeline",
	"GUIZHI_THEMED_RT_GOLDEN_DIR":    "themed-runtime",
	"GUIZHI_VIDEO_GOLDEN_DIR":        "video",
	"GUIZHI_WEBSNAP_GOLDEN_DIR":      "websnap",
	"GUIZHI_WECHAT_GOLDEN_DIR":       "wechat",
	"GUIZHI_WIKICOMPILER_GOLDEN_DIR": "wikicompiler",
}

func useFrozenGoldens() {
	for env, sub := range frozenGoldenDirs {
		if os.Getenv(env) != "" {
			continue
		}
		dir, err := filepath.Abs(filepath.Join("testdata", "golden", sub))
		if err != nil {
			continue
		}
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			os.Setenv(env, dir)
		}
	}
}

// 渲染/采集浏览器测试会把测试二进制本身当作采集子进程拉起（见 capture_browser.go），这里负责接管。
func TestMain(m *testing.M) {
	if runCaptureChildIfRequested() {
		return
	}
	useFrozenGoldens()
	// 冻结基准由 TS 实现在 UTC+8 的机器上生成：备份文件名、MCP 的 updated 日期等都按本地时区格式化。
	// 固定为 UTC+8（中国无夏令时），对拍结果就不依赖运行机器的时区——否则 UTC 的 CI 上会差 8 小时、日期错一天。
	time.Local = time.FixedZone("CST", 8*3600)
	os.Exit(m.Run())
}
