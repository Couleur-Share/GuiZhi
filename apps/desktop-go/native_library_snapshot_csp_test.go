package main

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"

	"guizhi-mygo-shell/viva/reader"
)

// 阅读岛的 CSP 只放行网页快照桥接脚本的那一个哈希（viva/reader.SnapshotBridgeSHA256）：
// 引擎里的桥接脚本一旦改动而这个常量没跟着改，快照在岛里会被 CSP 拦掉桥接脚本（没有高度、没有查找结果），所以在这里用真实文档锁住。
func TestSnapshotBridgeHashMatchesTheEngineDocument(t *testing.T) {
	ctx := t.Context()
	e, svc := snapshotAdapterEnv(t)
	name, hash := e.image("abc")
	e.version("v1", 1000, snapshotFor(name, hash, `<p>a</p><img src="local-image://`+name+`">`, ""), true)

	got, err := svc.WebSnapshot(ctx, e.itemID, "")
	if err != nil {
		t.Fatal(err)
	}
	doc := got.Document
	const open, closing = "<script>", "</script>"
	if strings.Count(doc, open) != 1 {
		t.Fatalf("文档里应恰有一个内联桥接脚本，实际 %d 个", strings.Count(doc, open))
	}
	start := strings.Index(doc, open) + len(open)
	end := strings.Index(doc[start:], closing)
	if end < 0 {
		t.Fatal("桥接脚本没有结束标签")
	}
	sum := sha256.Sum256([]byte(doc[start : start+end]))
	if want := base64.StdEncoding.EncodeToString(sum[:]); want != reader.SnapshotBridgeSHA256 {
		t.Fatalf("桥接脚本的 sha256 变了：文档里是 %s，viva/reader.SnapshotBridgeSHA256 是 %s（改常量，并重跑 readersmoke snapshot）", want, reader.SnapshotBridgeSHA256)
	}

	// 适配只动图片地址与 meta 的 img-src，桥接脚本（哈希）原样不动。
	adapted := reader.AdaptSnapshotDocument(doc)
	if strings.Contains(adapted, "local-image://") || !strings.Contains(adapted, "./media/local-image/"+name) {
		t.Errorf("本地图片地址应被换成岛内相对路径：%.300s", adapted)
	}
	if !strings.Contains(adapted, doc[start:start+end]) {
		t.Error("适配不能改动桥接脚本")
	}
	if strings.Contains(adapted, "'unsafe-inline'") {
		t.Error("适配后的文档不该含 unsafe-inline")
	}
}
