package themed

import (
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
)

func newDocs(t *testing.T) (*servicestest.ThemedFake, *DocLoader, *recLog) {
	t.Helper()
	svc := &servicestest.ThemedFake{}
	log := &recLog{}
	d := NewDocLoader(svc, kit.Guard{Notify: kit.NewNotifier(nil), Log: log}, nil)
	d.Spawn = runNow
	svc.PutPage(reqA, services.ThemedPage{ID: "v1"})
	svc.PutPage(reqB, services.ThemedPage{ID: "b1"})
	return svc, d, log
}

func renderIn(req services.ThemedRequest, version string) services.ThemedRenderInput {
	return services.ThemedRenderInput{ThemedRequest: req, VersionID: version, InstanceID: "inst"}
}

func TestDocLoaderLoadsOnceAndCaches(t *testing.T) {
	svc, d, _ := newDocs(t)
	in := renderIn(reqA, "v1")
	key := DocKey(in, 1, 0)
	for i := 0; i < 5; i++ {
		d.Want(in, key)
	}
	v := d.View()
	if v.Key != key || v.Doc != "<html>page v1 instance inst</html>" || v.VersionID != "v1" || v.Loading || v.Error != "" {
		t.Fatalf("快照不对：%+v", v)
	}
	if n := len(svc.Calls().Renders); n != 1 {
		t.Fatalf("同一份只该渲染一次，实际 %d", n)
	}
}

func TestDocLoaderUpdatedAtOrRevisionForcesReload(t *testing.T) {
	svc, d, _ := newDocs(t)
	in := renderIn(reqA, "v1")
	d.Want(in, DocKey(in, 1, 0))
	d.Want(in, DocKey(in, 2, 0)) // 同版本被更新（重做了图片）
	d.Want(in, DocKey(in, 2, 1)) // 预览的修订号变了
	if n := len(svc.Calls().Renders); n != 3 {
		t.Fatalf("三个不同的键应各渲染一次，实际 %d", n)
	}
}

func TestDocLoaderKeepsPreviousDocWhileSameArticleReloads(t *testing.T) {
	svc, d, _ := newDocs(t)
	in := renderIn(reqA, "v1")
	d.Want(in, DocKey(in, 1, 0))
	release, arrived := svc.Hold("render")
	next := renderIn(reqA, "v2")
	svc.PutPage(reqA, services.ThemedPage{ID: "v2"})
	d.Spawn = func(fn func()) { go fn() }
	got := d.Want(next, DocKey(next, 1, 0))
	<-arrived
	if got.Doc == "" || d.View().Doc == "" || d.View().Loading {
		t.Fatalf("同一篇换版本期间应保留上一份文档（不闪空白）：%+v", d.View())
	}
	if d.View().Key == DocKey(next, 1, 0) {
		t.Fatal("新文档到之前 Key 不该已是新的")
	}
	release()
	waitDoc(t, d, func(v DocView) bool { return v.VersionID == "v2" })
}

func TestDocLoaderClearsPreviousDocWhenArticleChanges(t *testing.T) {
	svc, d, _ := newDocs(t)
	a := renderIn(reqA, "v1")
	d.Want(a, DocKey(a, 1, 0))
	release, arrived := svc.Hold("render")
	d.Spawn = func(fn func()) { go fn() }
	b := renderIn(reqB, "b1")
	d.Want(b, DocKey(b, 1, 0))
	<-arrived
	if v := d.View(); v.Doc != "" || !v.Loading {
		t.Fatalf("换了文章不能把上一篇的页面留在画面上：%+v", v)
	}
	release()
	waitDoc(t, d, func(v DocView) bool { return v.VersionID == "b1" })
}

func TestDocLoaderErrorDoesNotRetryByItselfAndRetryRecovers(t *testing.T) {
	svc, d, log := newDocs(t)
	svc.Fail("render", errTest("renderer crashed"))
	in := renderIn(reqA, "v1")
	key := DocKey(in, 1, 0)
	for i := 0; i < 5; i++ {
		d.Want(in, key)
	}
	if v := d.View(); v.Error != "renderer crashed" || v.Loading || v.Doc != "" {
		t.Fatalf("失败应留下原因：%+v", v)
	}
	if n := len(svc.Calls().Renders); n != 1 {
		t.Fatalf("失败后不能每帧重试，实际渲染 %d 次", n)
	}
	if len(log.entries()) == 0 {
		t.Fatal("后台失败应留痕")
	}
	svc.Fail("render", nil)
	d.Retry()
	if v := d.View(); v.Error != "" || v.Doc == "" {
		t.Fatalf("重试成功后应有文档：%+v", v)
	}
}

func TestDocLoaderStaleResultIsDropped(t *testing.T) {
	svc, d, _ := newDocs(t)
	d.Spawn = func(fn func()) { go fn() }
	release, arrived := svc.Hold("render")
	a := renderIn(reqA, "v1")
	d.Want(a, DocKey(a, 1, 0))
	<-arrived // A 的读取卡在入口
	b := renderIn(reqB, "b1")
	d.Want(b, DocKey(b, 1, 0))
	waitDoc(t, d, func(v DocView) bool { return v.VersionID == "b1" })
	release() // A 的结果此刻才到：它已被取代，不能盖掉 B
	time.Sleep(50 * time.Millisecond)
	if v := d.View(); v.VersionID != "b1" {
		t.Fatalf("旧读取的结果盖掉了新的：%+v", v)
	}
}

func TestDocLoaderResetInvalidatesInFlightAndStartsOver(t *testing.T) {
	svc, d, _ := newDocs(t)
	d.Spawn = func(fn func()) { go fn() }
	release, arrived := svc.Hold("render")
	a := renderIn(reqA, "v1")
	d.Want(a, DocKey(a, 1, 0))
	<-arrived
	d.Reset()
	release()
	time.Sleep(50 * time.Millisecond)
	if v := d.View(); v.Doc != "" || v.Key != "" {
		t.Fatalf("Reset 后在途结果应作废：%+v", v)
	}
	d.Spawn = runNow
	d.Want(a, DocKey(a, 1, 0))
	if v := d.View(); v.VersionID != "v1" {
		t.Fatalf("Reset 后同一个键应重新读取：%+v", v)
	}
}

func TestDocLoaderPropagatesImagesOmitted(t *testing.T) {
	svc, d, _ := newDocs(t)
	svc.SetImagesOmitted(true)
	in := renderIn(reqA, "v1")
	d.Want(in, DocKey(in, 1, 0))
	if !d.View().ImagesOmitted {
		t.Fatal("ImagesOmitted 应原样带出")
	}
}

func TestDocLoaderRetryBeforeAnyWantDoesNothing(t *testing.T) {
	svc, d, _ := newDocs(t)
	d.Retry()
	if n := len(svc.Calls().Renders); n != 0 {
		t.Fatalf("没有想要的文档时 Retry 不该请求，实际 %d", n)
	}
}

func waitDoc(t *testing.T, d *DocLoader, cond func(DocView) bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond(d.View()) {
		if time.Now().After(deadline) {
			t.Fatalf("等待超时：%+v", d.View())
		}
		time.Sleep(time.Millisecond)
	}
}
