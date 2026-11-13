//go:build !windows

package main

import "syscall"

// statIdentity：非 Windows 平台按 stat 取设备号 / inode / 创建时间（无创建时间的平台为 0）。
func statIdentity(path string) (fileIdentity, error) {
	var st syscall.Stat_t
	if err := syscall.Lstat(path, &st); err != nil {
		return fileIdentity{}, err
	}
	return fileIdentity{Device: uint64(st.Dev), Inode: uint64(st.Ino), Nlink: uint64(st.Nlink)}, nil
}

func systemDiskSpace(root string) (int64, int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(root, &st); err != nil {
		return 0, 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), int64(st.Bsize), nil
}
