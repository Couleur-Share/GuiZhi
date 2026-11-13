package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// shotResultPath 返回 -shotjs 结果文件路径：<截图路径去扩展名>.<pid>.result.json。
// 带 pid 是因为恢复备份等操作会重启进程（子进程沿用同样的命令行参数），各进程的结果不能互相覆盖。
func shotResultPath(shotPath string, pid int) string {
	base := shotPath
	if i := strings.LastIndex(base, "."); i > strings.LastIndexAny(base, `/\`) {
		base = base[:i]
	}
	return fmt.Sprintf("%s.%d.result.json", base, pid)
}

// writeShotResult 把 -shotjs 脚本的返回值（或错误）以 JSON 写入结果文件，供验证脚本机器读取。
// 重启后的子进程标准输出不会被父进程接管，所以必须落盘。
func writeShotResult(shotPath string, value any, evalErr error) {
	rec := map[string]any{"value": value}
	if evalErr != nil {
		rec["error"] = evalErr.Error()
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		data, _ = json.Marshal(map[string]any{"error": "结果无法序列化：" + err.Error()})
	}
	_ = os.WriteFile(shotResultPath(shotPath, os.Getpid()), data, 0o644)
}
