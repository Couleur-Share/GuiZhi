//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// 键路径与 MyGo 壳（mygo/internal/windows/login.go）一致：Run 键里一个值，任务管理器的「启动」页用 StartupApproved 关掉它。
const (
	runKeyPath      = `Software\Microsoft\Windows\CurrentVersion\Run`
	approvedKeyPath = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`
)

// osCommandLineArgs 读操作系统给进程的原始命令行（GetCommandLineW + CommandLineToArgvW，不含 argv[0]）。
// mygo 的 init() 已把登录项参数从 os.Args 摘掉，只有这里还看得到。
func osCommandLineArgs() []string {
	args, err := windows.DecomposeCommandLine(windows.UTF16PtrToString(windows.GetCommandLine()))
	if err != nil || len(args) == 0 {
		return append([]string(nil), os.Args[1:]...)
	}
	return args[1:]
}

// resolvedExecutable 是当前可执行文件的真实路径（解开符号链接，与 MyGo 一致）。
func resolvedExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return exe, nil
}

// setVivaLoginItem 开启或关闭当前用户的登录项（生产路径：HKCU 的正式 Run 键）。
func setVivaLoginItem(open bool) error {
	exe, err := resolvedExecutable()
	if err != nil {
		return fmt.Errorf("取不到可执行文件路径：%w", err)
	}
	return applyRegistryLoginItem(registry.CURRENT_USER, runKeyPath, approvedKeyPath, webview2AppName, exe, open)
}

// applyRegistryLoginItem 在指定根键与子键下读写登录项（测试只在临时子键下调用，绝不碰真实的 Run 键）。
// 开启：写入启动命令，并清掉任务管理器留下的「已禁用」标记（用户在应用里重新打开等于重新要求自启）；关闭：删除两处的值。
func applyRegistryLoginItem(root registry.Key, runSub, approvedSub, name, exePath string, open bool) error {
	if !open {
		if err := deleteRegistryValue(root, runSub, name); err != nil {
			return err
		}
		return deleteRegistryValue(root, approvedSub, name)
	}
	k, _, err := registry.CreateKey(root, runSub, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开登录项注册表键失败：%w", err)
	}
	err = k.SetStringValue(name, loginCommandLine(exePath))
	k.Close()
	if err != nil {
		return fmt.Errorf("写入登录项失败：%w", err)
	}
	return deleteRegistryValue(root, approvedSub, name)
}

// deleteRegistryValue 删除一个值；键或值不存在视为成功。
func deleteRegistryValue(root registry.Key, sub, name string) error {
	k, err := registry.OpenKey(root, sub, registry.SET_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("打开注册表键失败：%w", err)
	}
	defer k.Close()
	if err := k.DeleteValue(name); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("删除登录项失败：%w", err)
	}
	return nil
}
