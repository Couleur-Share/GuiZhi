package main

// 真实安装包产物 × 更新器核心（默认跳过）：用 build-installer.ps1 生成的 latest.yml 与安装包，
// 经本地 httptest 走一遍「检查 → 下载 → sha512 校验 → 启动参数」，确认现有更新器接受这份产物。
// 安装包不会被执行（spawnInstaller 为桩）。
//   GUIZHI_REAL_INSTALLER_DIR=<spikes\mygo-shell\out\installer> go test -run TestUpdaterAcceptsRealInstallerArtifacts -v

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdaterAcceptsRealInstallerArtifacts(t *testing.T) {
	dir := os.Getenv("GUIZHI_REAL_INSTALLER_DIR")
	if dir == "" {
		t.Skip("未设置 GUIZHI_REAL_INSTALLER_DIR")
	}
	yml, err := os.ReadFile(filepath.Join(dir, "latest.yml"))
	if err != nil {
		t.Fatal(err)
	}
	exes, _ := filepath.Glob(filepath.Join(dir, "GuiZhi-Setup-*.exe"))
	if len(exes) != 1 {
		t.Fatalf("应恰有一个安装包，实际 %v", exes)
	}
	exe, err := os.ReadFile(exes[0])
	if err != nil {
		t.Fatal(err)
	}
	f := newUpdaterFixture(t, "0.27.0")
	f.installer = exe
	f.manifest = func(string) string { return string(yml) }
	ctx := context.Background()

	res := f.u.Check(ctx, rawOpts(map[string]any{"channel": "stable", "trigger": "startup"}))
	if res["success"] != true || res["updateAvailable"] != true {
		t.Fatalf("真实 latest.yml 未被接受：%v", res)
	}
	if got := res["result"].(map[string]any)["updateInfo"].(map[string]any)["version"]; got != "0.28.0" {
		t.Fatalf("版本 = %v", got)
	}
	if r := f.u.Download(ctx, rawOpts(map[string]any{"channel": "stable"})); r["success"] != true {
		t.Fatalf("下载/校验失败：%v", r)
	}
	if sum, _ := fileSHA512(f.u.installerPath); sum != sha512b64(exe) {
		t.Fatal("下载后的文件校验和不一致")
	}
	if !strings.HasSuffix(f.u.installerPath, ".exe") {
		t.Fatalf("安装包路径 %s", f.u.installerPath)
	}
	t.Logf("更新器接受真实产物：%s（%d 字节）", filepath.Base(exes[0]), len(exe))
}
