package main

import (
	"os"
	"path/filepath"
	"testing"
)

// 阅读岛的本地媒体：只给用户数据目录 images 下白名单扩展名的图片、单张不超过上限；其余一律不给。
func TestReaderMediaServesOnlyWhitelistedImagesInsideTheDirectory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	images := filepath.Join(root, "images")
	if err := os.MkdirAll(filepath.Join(images, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path string, size int) {
		t.Helper()
		if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(images, "a.png"), 10)
	write(filepath.Join(images, "b.JPG"), 10)
	write(filepath.Join(images, "sub", "c.webp"), 10)
	write(filepath.Join(images, "note.txt"), 10)
	write(filepath.Join(images, "vector.svg"), 10)
	write(filepath.Join(images, "big.png"), 101)
	write(filepath.Join(root, "outside.png"), 10)
	if err := os.MkdirAll(filepath.Join(images, "folder.png"), 0o755); err != nil {
		t.Fatal(err)
	}
	media := readerMediaFromDir(images, 100)

	for _, c := range []struct {
		name, scheme, path string
		ok                 bool
		mime               string
	}{
		{"png", "local-image", "a.png", true, "image/png"},
		{"扩展名大小写不敏感", "local-image", "b.JPG", true, "image/jpeg"},
		{"子目录", "local-image", "sub/c.webp", true, "image/webp"},
		{"文本文件不给", "local-image", "note.txt", false, ""},
		{"svg 不给（可带脚本）", "local-image", "vector.svg", false, ""},
		{"超过上限不给", "local-image", "big.png", false, ""},
		{"不存在", "local-image", "missing.png", false, ""},
		{"目录不给", "local-image", "folder.png", false, ""},
		{"越出目录", "local-image", "../outside.png", false, ""},
		{"反斜杠越出目录", "local-image", `..\outside.png`, false, ""},
		{"绝对路径", "local-image", "/a.png", false, ""},
		{"盘符与流名", "local-image", "a.png:evil", false, ""},
		{"视频方案不给", "local-video", "a.png", false, ""},
		{"未知方案不给", "file", "a.png", false, ""},
	} {
		mime, data, ok := media(c.scheme, c.path)
		if ok != c.ok || mime != c.mime || (ok && len(data) == 0) {
			t.Errorf("%s：(%q, %d 字节, %v)，应为 (%q, ok=%v)", c.name, mime, len(data), ok, c.mime, c.ok)
		}
	}

	if _, data, ok := readerMediaFromDir(images, 101)("local-image", "big.png"); !ok || len(data) != 101 {
		t.Errorf("上限恰好等于文件大小时应给出整个文件：%d 字节 %v", len(data), ok)
	}
}
