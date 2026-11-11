//go:build !windows

package main

// 非 Windows 不提供系统密钥环（官方仅支持 Windows 11 x64）：自动完整备份保持暂停，只能用恢复口令。
func newKeyProtector(string) keyProtector { return nil }
