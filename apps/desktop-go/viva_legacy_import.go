package main

// Viva 首次启动导入旧版 React 渲染层的设置与界面偏好（localStorage）。
//
// 旧数据在旧版壳的 WebView2 配置里：%LOCALAPPDATA%\GuiZhi\WebView2。Viva 的阅读岛/媒体岛把临时 WebView2 profile 放在
// %LOCALAPPDATA%\guizhi-viva\WebView2（窗口类名 vivaWindowClass），与它物理隔离：Viva 不会改写旧数据，
// 所以首次启动导入是只读复制，随时可以回退到旧版。导入的规则、提交点与幂等见 native_settings_import.go。

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"
)

// vivaLegacyImportTimeout 是首次导入的上限：读 leveldb 与写设置表，正常毫秒级；卡住时不能挡住窗口出现。
const vivaLegacyImportTimeout = 30 * time.Second

// vivaLegacyWebView2Folder 选出要读的旧版 WebView2 配置目录。
// 手工用 -userdata / -db 指向临时目录试跑时，不能去读本机真实安装的配置（那会把真实设置导入临时数据）：
// 此时取该次运行 userData 下的 webview2 目录，除非调用方用 WEBVIEW2_USER_DATA_FOLDER 明确指了位置。
func vivaLegacyWebView2Folder(explicitData bool, getenv func(string) string, userData string, current func() string) string {
	if getenv("WEBVIEW2_USER_DATA_FOLDER") != "" || !explicitData {
		return current()
	}
	return filepath.Join(userData, "webview2")
}

// importVivaLegacy 执行一次首次启动导入。已初始化时什么都不做；失败只记日志与 error.log，不阻断启动
// （失败不会写 ui-prefs.json，下次启动自动重试）。返回导入错误供测试断言。
func importVivaLegacy(ctx context.Context, lib *Library, nset *nativeSettings, webview2Folder string) error {
	ctx, cancel := context.WithTimeout(ctx, vivaLegacyImportTimeout)
	defer cancel()
	if err := nset.importLegacy(ctx, webview2Folder, lib.paths.UserData); err != nil {
		log.Printf("[native-settings] 导入旧版设置失败：%v", err)
		lib.paths.logAppError("native-settings", "导入旧版设置", err.Error(), nil)
		return err
	}
	return nil
}

// importLegacySettings 按当前进程的环境与命令行选出旧版配置目录并导入（真实实例启动时调用一次）。
func (vb vivaBoot) importLegacySettings(nset *nativeSettings) {
	explicit := flagGiven("userdata") || flagGiven("db")
	folder := vivaLegacyWebView2Folder(explicit, os.Getenv, vb.lib.paths.UserData, currentWebView2UserDataFolder)
	_ = importVivaLegacy(context.Background(), vb.lib, nset, folder)
}
