package main

// 对应 funasr-update-paths.ts / funasr-update-storage.ts 的文件系统边界：
// 目录身份（路径字符串相同不代表仍是原来的目录）、拒绝链接与硬链接、按分配单位估算空间。

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// directoryIdentity 与 TS 的 DirectoryIdentity 同形同值（libuv：卷序列号 / 文件索引 / 创建时间 ns），
// 因为备份清单会落盘，Go 与旧 Electron 版要能互认对方留下的备份。
type directoryIdentity struct {
	Device    string `json:"device"`
	Inode     string `json:"inode"`
	Birthtime string `json:"birthtime"`
}

// fileIdentity 由平台实现（funasr_fs_windows.go / funasr_fs_other.go）。
type fileIdentity struct {
	Device, Inode uint64
	BirthNs       int64
	Nlink         uint64
}

func readDirectoryIdentity(directory string) (directoryIdentity, error) {
	st, err := os.Lstat(directory)
	if err != nil {
		return directoryIdentity{}, err
	}
	if !st.IsDir() || isLinkMode(st.Mode()) {
		return directoryIdentity{}, fmt.Errorf("不是可验证身份的普通目录，保留文件：%s", directory)
	}
	id, err := statIdentity(directory)
	if err != nil {
		return directoryIdentity{}, err
	}
	if id.Inode == 0 {
		return directoryIdentity{}, fmt.Errorf("不是可验证身份的普通目录，保留文件：%s", directory)
	}
	return directoryIdentity{
		Device:    strconv.FormatUint(id.Device, 10),
		Inode:     strconv.FormatUint(id.Inode, 10),
		Birthtime: strconv.FormatInt(id.BirthNs, 10),
	}, nil
}

func assertDirectoryIdentity(directory string, expected directoryIdentity) error {
	actual, err := readDirectoryIdentity(directory)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("目录身份发生变化，保留文件：%s", directory)
	}
	return nil
}

// assertPlainFunasrTree 拒绝目录连接、符号链接和硬链接；遇到不认识的节点就停，不递归到目标。
func assertPlainFunasrTree(directory string) error {
	pending := []string{directory}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		identity, err := readDirectoryIdentity(current)
		if err != nil {
			return err
		}
		entries, err := os.ReadDir(current)
		if err != nil {
			return err
		}
		for _, e := range entries {
			file := filepath.Join(current, e.Name())
			st, err := os.Lstat(file)
			if err != nil {
				return err
			}
			if isLinkMode(st.Mode()) {
				return fmt.Errorf("发现链接或异常文件，保留整个目录：%s", file)
			}
			if st.IsDir() {
				pending = append(pending, file)
				continue
			}
			if !st.Mode().IsRegular() {
				return fmt.Errorf("发现链接或异常文件，保留整个目录：%s", file)
			}
			id, err := statIdentity(file)
			if err != nil {
				return err
			}
			if id.Nlink != 1 {
				return fmt.Errorf("发现链接或异常文件，保留整个目录：%s", file)
			}
		}
		if err := assertDirectoryIdentity(current, identity); err != nil {
			return err
		}
	}
	return nil
}

// ── 空间预算 ──

const (
	funasrGiB            = int64(1) << 30
	funasrReserveBytes   = int64(512) << 20
	funasrBytesFormatDiv = float64(1 << 30)
)

type funasrSpaceBudget struct{ BackupBytes, WorkBytes int64 }

func formatFunasrBytes(bytes int64) string {
	return fmt.Sprintf("%.2f GiB", float64(bytes)/funasrBytesFormatDiv)
}

// measureFunasrEnvironment 按目标卷分配单位向上取整；不跟随链接，防止环境路径跳到其他卷。
func measureFunasrEnvironment(directory string, blockSize int64) (int64, error) {
	root, err := os.Lstat(directory)
	if err != nil {
		return 0, err
	}
	if !root.IsDir() || isLinkMode(root.Mode()) {
		return 0, errors.New("引擎环境不是普通目录，无法安全备份")
	}
	total := blockSize
	pending := []string{directory}
	for len(pending) > 0 {
		dir := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		entries, err := os.ReadDir(dir)
		if err != nil {
			return 0, err
		}
		for _, e := range entries {
			file := dir + "/" + e.Name()
			st, err := os.Lstat(filepath.Join(dir, e.Name()))
			if err != nil {
				return 0, err
			}
			switch {
			case isLinkMode(st.Mode()):
				return 0, fmt.Errorf("引擎环境包含链接，无法估算备份空间：%s", file)
			case st.IsDir():
				pending = append(pending, filepath.Join(dir, e.Name()))
				total += blockSize
			case st.Mode().IsRegular():
				total += (st.Size() + blockSize - 1) / blockSize * blockSize
			default:
				return 0, fmt.Errorf("引擎环境包含不支持的文件：%s", file)
			}
		}
	}
	return total, nil
}

// diskSpaceFunc 查询目录所在卷的可用字节与分配单位；测试里可替换。
type diskSpaceFunc func(root string) (available, blockSize int64, err error)

func readDiskSpace(fn diskSpaceFunc, root string) (int64, int64, error) {
	available, blockSize, err := fn(root)
	if err == nil && (available < 0 || blockSize <= 0) {
		err = errors.New("磁盘返回了无效容量")
	}
	if err != nil {
		return 0, 0, fmt.Errorf("无法检查引擎磁盘空间，更新尚未开始：%v", err)
	}
	return available, blockSize, nil
}

func assertFunasrUpdateSpace(fn diskSpaceFunc, root string, required int64) error {
	available, _, err := readDiskSpace(fn, root)
	if err != nil {
		return err
	}
	if available < required {
		return fmt.Errorf("磁盘空间不足，更新尚未开始：%s 所在磁盘预计需要 %s，当前可用 %s。请释放空间后重试。", root, formatFunasrBytes(required), formatFunasrBytes(available))
	}
	return nil
}

func planFunasrUpdateSpace(fn diskSpaceFunc, root, env string) (funasrSpaceBudget, error) {
	_, blockSize, err := readDiskSpace(fn, root)
	if err != nil {
		return funasrSpaceBudget{}, err
	}
	backup, err := measureFunasrEnvironment(env, blockSize)
	if err != nil {
		return funasrSpaceBudget{}, err
	}
	// 更新包大小在解析依赖前未知：预留两份环境大小（至少 1 GiB）及 512 MiB 余量。
	// 这是保守预算，不是容量保证；复制结束后再次检查，实际写满仍由事务回滚。
	work := max(backup*2, funasrGiB) + funasrReserveBytes
	if err := assertFunasrUpdateSpace(fn, root, backup+work); err != nil {
		return funasrSpaceBudget{}, err
	}
	return funasrSpaceBudget{BackupBytes: backup, WorkBytes: work}, nil
}
