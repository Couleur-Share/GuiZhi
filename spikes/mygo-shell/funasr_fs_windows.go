//go:build windows

package main

import (
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// statIdentity 按 libuv 的口径取身份：dev=卷序列号，ino=64 位文件索引，birthtime=创建时间（ns，Unix 纪元）。
func statIdentity(path string) (fileIdentity, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fileIdentity{}, err
	}
	h, err := windows.CreateFile(p, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return fileIdentity{}, err
	}
	defer windows.CloseHandle(h)
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return fileIdentity{}, err
	}
	ft := uint64(info.CreationTime.HighDateTime)<<32 | uint64(info.CreationTime.LowDateTime)
	const epochDiff = 116444736000000000 // 1601→1970，100ns 单位
	birth := int64((ft - epochDiff) / 10000000 * 1000000000)
	birth += int64((ft - epochDiff) % 10000000 * 100)
	return fileIdentity{
		Device:  uint64(info.VolumeSerialNumber),
		Inode:   uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow),
		BirthNs: birth,
		Nlink:   uint64(info.NumberOfLinks),
	}, nil
}

var procGetDiskFreeSpaceW = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetDiskFreeSpaceW")

// systemDiskSpace 对应 fs.statfs（libuv 用 GetDiskFreeSpaceW）：可用字节 = 空闲簇 × 簇大小。
func systemDiskSpace(root string) (int64, int64, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return 0, 0, err
	}
	vol := make([]uint16, windows.MAX_PATH+1)
	p, err := windows.UTF16PtrFromString(abs)
	if err != nil {
		return 0, 0, err
	}
	if err := windows.GetVolumePathName(p, &vol[0], uint32(len(vol))); err != nil {
		return 0, 0, err
	}
	var sectors, bytesPer, free, total uint32
	r, _, e := procGetDiskFreeSpaceW.Call(uintptr(unsafe.Pointer(&vol[0])),
		uintptr(unsafe.Pointer(&sectors)), uintptr(unsafe.Pointer(&bytesPer)),
		uintptr(unsafe.Pointer(&free)), uintptr(unsafe.Pointer(&total)))
	if r == 0 {
		if e == syscall.Errno(0) {
			e = syscall.EINVAL
		}
		return 0, 0, e
	}
	block := int64(sectors) * int64(bytesPer)
	return int64(free) * block, block, nil
}
