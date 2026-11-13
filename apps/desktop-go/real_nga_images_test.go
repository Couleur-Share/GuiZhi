package main

// 线上确认（默认跳过）：产品的 NGA 附件图下载路径（ngaDefaultDownloadImage）是否已不再被 img.nga.cn 判 567。
// 只请求 GUIZHI_REAL_NGA_IMAGES 里给出的图片地址（逗号分隔），每张 1 次、间隔 3 秒，不访问 bbs.nga.cn。
//   GUIZHI_REAL_NET=1 GUIZHI_REAL_NGA_IMAGES=<url,url,...> go test -run TestRealNgaImages -v

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRealNgaImages(t *testing.T) {
	list := os.Getenv("GUIZHI_REAL_NGA_IMAGES")
	if os.Getenv("GUIZHI_REAL_NET") != "1" || list == "" {
		t.Skip("未设置 GUIZHI_REAL_NET=1 与 GUIZHI_REAL_NGA_IMAGES")
	}
	l := &Library{}
	for i, u := range strings.Split(list, ",") {
		if i > 0 {
			time.Sleep(3 * time.Second)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		dir, file, err := l.ngaDefaultDownloadImage(ctx, strings.TrimSpace(u))
		cancel()
		if err != nil {
			t.Errorf("图片 %d 下载失败：%v", i+1, err)
			continue
		}
		fi, _ := os.Stat(file)
		t.Logf("图片 %d 成功：%s，%d 字节", i+1, strings.TrimPrefix(file, dir), fi.Size())
		_ = os.RemoveAll(dir)
	}
}
