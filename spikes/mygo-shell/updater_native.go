package main

// 更新器与 MyGo / 操作系统的接线：推送、打开、退出，以及脱离父进程启动安装包。

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/egoist/mygo"
)

// defaultChangelogPaths 随包的 CHANGELOG.md 候选位置（可执行文件旁的 resources/ 或同级）。
func defaultChangelogPaths() []string {
	var out []string
	if p := os.Getenv("GUIZHI_CHANGELOG"); p != "" {
		out = append(out, p)
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		out = append(out, filepath.Join(dir, "resources", "CHANGELOG.md"), filepath.Join(dir, "CHANGELOG.md"))
	}
	return out
}

// packagedBuild 由打包脚本 -ldflags "-X main.packagedBuild=true" 注入；对应 Electron 的 app.isPackaged。
var packagedBuild = "false"

func (l *Library) registerUpdater(b *Bridge, isPackaged bool, quit func()) *Updater {
	localApp := os.Getenv("LOCALAPPDATA")
	if localApp == "" {
		localApp = os.TempDir()
	}
	home, _ := os.UserHomeDir()
	u := newUpdater(updaterDeps{
		currentVersion: func() string { return appVersion },
		isDev:          func() bool { return !isPackaged },
		cacheDir:       filepath.Join(localApp, "guizhi-updater"),
		changelogPaths: defaultChangelogPaths,
		push:           l.pushMain,
		openExternal:   func(s string) error { return mygo.Shell.OpenExternal(s) },
		openPath:       func(s string) error { return mygo.Shell.OpenPath(s) },
		showInFolder:   func(s string) { mygo.Shell.ShowItemInFolder(s) },
		downloadsDir:   func() string { return filepath.Join(home, "Downloads") },
		spawnInstaller: spawnDetached,
		quit:           quit,
		createBackup: func(reason string) {
			// 安装前先留一份可回退的快照；失败只记日志，不阻断更新（与 TS 的 createBackupSafe 一致）
			if _, err := l.localBackups().createSafe(reason); err != nil {
				logAppErrorf("updater", "安装前备份失败", "%v", err)
			}
		},
	})
	u.register(b)
	return u
}

func spawnDetached(path string, args []string) error {
	cmd := exec.Command(path, args...)
	detachProcess(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	log.Printf("[updater] 已启动安装包 pid=%d", cmd.Process.Pid)
	return cmd.Process.Release()
}
