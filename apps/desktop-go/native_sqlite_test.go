package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 库是否在位：可执行文件同目录、或环境变量指定的文件存在即算在位。
func TestNativeSQLiteMissing(t *testing.T) {
	dir := t.TempDir()
	if !nativeSQLiteMissing("", dir) {
		t.Fatal("目录里没有 Sqlitecore.dll，应判定缺失")
	}
	if !nativeSQLiteMissing("", "") {
		t.Fatal("连可执行文件目录都不知道时，应判定缺失")
	}
	lib := filepath.Join(dir, nativeSQLiteFile)
	if err := os.WriteFile(lib, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if nativeSQLiteMissing("", dir) {
		t.Fatal("可执行文件同目录有库，不应判定缺失")
	}

	// 环境变量指向存在的文件也算在位；指向不存在的文件则回头看同目录。
	other := t.TempDir()
	envLib := filepath.Join(other, "elsewhere.dll")
	if err := os.WriteFile(envLib, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if nativeSQLiteMissing(envLib, other) {
		t.Fatal("环境变量指向的文件存在，不应判定缺失")
	}
	if !nativeSQLiteMissing(filepath.Join(other, "nope.dll"), other) {
		t.Fatal("环境变量指向的文件不存在且同目录也没有，应判定缺失")
	}
	// 目录同名不算文件。
	if err := os.Mkdir(filepath.Join(other, nativeSQLiteFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if !nativeSQLiteMissing("", other) {
		t.Fatal("同名目录不是库文件，应判定缺失")
	}
}

// 测试进程的可执行文件目录里没有 Sqlitecore.dll：错误会被加上原因，且保留原始错误链。
func TestExplainSQLiteOpenErrorMissing(t *testing.T) {
	t.Setenv("MYGO_NATIVE_SQLITECORE", "")
	if explainSQLiteOpenError(nil) != nil {
		t.Fatal("nil 应原样返回")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Skip("取不到测试进程路径")
	}
	if !nativeSQLiteMissing("", filepath.Dir(exe)) {
		t.Skip("测试二进制旁恰好有 Sqlitecore.dll，无法构造缺库场景")
	}
	cause := errors.New("native: ffi: Sqlitecore has no build for windows-amd64")
	got := explainSQLiteOpenError(cause)
	if !errors.Is(got, cause) {
		t.Fatalf("必须保留原始错误链：%v", got)
	}
	for _, want := range []string{"Sqlitecore.dll", "GuiZhi.exe", "安全软件", "原始错误"} {
		if !strings.Contains(got.Error(), want) {
			t.Errorf("说明里缺少 %q：%v", want, got)
		}
	}

	// 环境变量指向一个真实存在的文件：库“在位”，不改写错误。
	envLib := filepath.Join(t.TempDir(), "Sqlitecore.dll")
	if err := os.WriteFile(envLib, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MYGO_NATIVE_SQLITECORE", envLib)
	if got := explainSQLiteOpenError(cause); got != cause {
		t.Fatalf("库在位时不应改写真正的错误：%v", got)
	}
}
