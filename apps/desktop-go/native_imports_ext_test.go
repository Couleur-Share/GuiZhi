package main

// docSiteAdapter 与 platformAuthAdapter 的测试：
// 文档站批次经真实的 crawl:* 处理器、真实 SQLite 与本地 httptest 站点往返；
// 线路上的异常形状（旧记录里非字符串的合集、空载荷、没有原因的失败）用登记在假 Bridge 上的处理器覆盖；
// 平台账号经真实的会话管理读状态，登录请求的形状与取消 NodeSeek 验证的路径各自守住。

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
)

// newDocSiteEnv 在真实的临时库上登记 crawl:* 处理器，批次抓的是本地 httptest 站点；不依赖黄金库。
func newDocSiteEnv(t *testing.T) (services.DocSite, *crawlSite) {
	t.Helper()
	_, paths, db := openAdapterTestDB(t)
	site := newCrawlSite(t)
	l := &Library{db: db, dbPath: paths.DatabasePath(), paths: paths, webFetcher: site.fx.f}
	b := newSystemBridge()
	l.registerCrawl(b)
	t.Cleanup(func() { l.crawl.close(context.Background()) })
	return docSiteAdapter{b: b}, site
}

// waitJob 轮询批次明细直到满足条件，超时报错。
func waitJob(t *testing.T, svc services.DocSite, id string, ok func(services.CrawlJobDetail) bool) services.CrawlJobDetail {
	t.Helper()
	deadline := time.Now().Add(40 * time.Second)
	for {
		detail, err := svc.GetJob(context.Background(), id)
		if err != nil {
			t.Fatalf("读取批次 %s：%v", id, err)
		}
		if ok(detail) {
			return detail
		}
		if time.Now().After(deadline) {
			t.Fatalf("批次 %s 未在时限内满足条件：%+v", id, detail.Job)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func pageByURL(pages []services.CrawlPage, url string) (services.CrawlPage, bool) {
	for _, p := range pages {
		if p.URL == url {
			return p, true
		}
	}
	return services.CrawlPage{}, false
}

// 一个文档站批次经适配器走完：创建 → 列表里有它（入口与目录范围原样回读）→ 明细按页给出状态 → 终态计数与页面状态一致。
func TestNativeImportsDocSiteBatchAgainstRealHandlers(t *testing.T) {
	ctx := t.Context()
	svc, site := newDocSiteEnv(t)
	base := site.base

	id, err := svc.CreateJob(ctx, services.CrawlInput{
		Purpose: "documents", MaxPages: 50, MaxDepth: 2, DuplicatePolicy: services.CrawlDuplicateSkip,
		Seeds: []services.CrawlSeed{{URL: base + "/docs/guide/index", Mode: services.CrawlSeedDirectory}},
	})
	if err != nil || id == "" {
		t.Fatalf("创建批次：%q %v", id, err)
	}

	detail := waitJob(t, svc, id, func(d services.CrawlJobDetail) bool {
		return d.Job != nil && d.Job.Status == services.CrawlJobCompleted
	})
	if p, ok := pageByURL(detail.Pages, base+"/docs/guide/index"); !ok || p.Status != services.CrawlPageAdded || p.ItemID == "" || p.JobID != id {
		t.Errorf("入口页应已新增并关联条目：%+v", p)
	}
	if p, ok := pageByURL(detail.Pages, base+"/docs/guide/secret"); !ok || p.Status != services.CrawlPageSkipped {
		t.Errorf("robots 禁止的页面应被跳过：%+v", p)
	}
	if _, ok := pageByURL(detail.Pages, base+"/other/z"); ok {
		t.Error("目录范围外的页面不应进入批次")
	}
	counted := map[string]int{}
	for _, p := range detail.Pages {
		counted[p.Status]++
	}
	for status, n := range counted {
		if detail.Job.Counts[status] != n {
			t.Errorf("计数 %s=%d 与明细里的 %d 页不一致", status, detail.Job.Counts[status], n)
		}
	}

	jobs, err := svc.ListJobs(ctx)
	if err != nil || len(jobs) != 1 || jobs[0].ID != id {
		t.Fatalf("批次列表：%+v %v", jobs, err)
	}
	in := jobs[0].Input
	if in.Purpose != "documents" || in.MaxPages != 50 || in.MaxDepth != 2 || in.DuplicatePolicy != services.CrawlDuplicateSkip || in.CollectionID != nil {
		t.Errorf("输入回读：%+v", in)
	}
	if len(in.Seeds) != 1 || in.Seeds[0].Mode != services.CrawlSeedDirectory || in.Seeds[0].Directory != "/docs/guide/" || in.Seeds[0].URL != base+"/docs/guide/index" {
		t.Errorf("入口回读（目录范围应由处理器补成入口所在目录）：%+v", in.Seeds)
	}
	if jobs[0].Status != services.CrawlJobCompleted || jobs[0].Error != "" {
		t.Errorf("终态：%+v", jobs[0])
	}
}

// 暂停 → 继续 → 取消：状态逐步变化；取消之后不可再继续，原因带回；缺失批次的暂停与读取行为与处理器一致。
func TestNativeImportsDocSiteControlsAgainstRealHandlers(t *testing.T) {
	ctx := t.Context()
	svc, site := newDocSiteEnv(t)
	slow := site.base + "/slow/page"

	id, err := svc.CreateJob(ctx, services.CrawlInput{
		Purpose: "documents", MaxPages: 5, MaxDepth: 0, DuplicatePolicy: services.CrawlDuplicateSkip,
		Seeds: []services.CrawlSeed{{URL: slow, Mode: services.CrawlSeedPage}},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, svc, id, func(d services.CrawlJobDetail) bool {
		p, ok := pageByURL(d.Pages, slow)
		return ok && p.Status == services.CrawlPageRunning
	})
	if err := svc.PauseJob(ctx, id); err != nil {
		t.Fatalf("暂停：%v", err)
	}
	waitJob(t, svc, id, func(d services.CrawlJobDetail) bool { return d.Job != nil && d.Job.Status == services.CrawlJobPaused })

	if err := svc.CancelJob(ctx, id); err != nil {
		t.Fatalf("取消：%v", err)
	}
	detail, err := svc.GetJob(ctx, id)
	if err != nil || detail.Job == nil || detail.Job.Status != services.CrawlJobCanceled {
		t.Fatalf("取消后的批次：%+v %v", detail.Job, err)
	}
	if err := svc.ResumeJob(ctx, id); err == nil || err.Error() != "批次不存在或已经取消" {
		t.Errorf("已取消的批次不可继续：%v", err)
	}
	if err := svc.PauseJob(ctx, "missing"); err == nil || err.Error() != "采集批次不存在" {
		t.Errorf("暂停缺失批次：%v", err)
	}
	if detail, err := svc.GetJob(ctx, "missing"); err != nil || detail.Job != nil || detail.Pages == nil || len(detail.Pages) != 0 {
		t.Errorf("读取缺失批次应是 Job 为 nil、页面为空：%+v %v", detail, err)
	}
	if err := svc.RetryJob(ctx, strings.Repeat("x", 101)); err == nil || err.Error() != "记录 ID 无效" {
		t.Errorf("过长的批次 id：%v", err)
	}
}

// 创建被拒绝时，处理器的原因原样带回（界面把它放进可复制的详情）：研究批次、不存在的合集、入口数量不对、数值越界。
func TestNativeImportsDocSiteCreateRejections(t *testing.T) {
	ctx := t.Context()
	svc, site := newDocSiteEnv(t)
	seed := []services.CrawlSeed{{URL: site.base + "/docs/guide/index", Mode: services.CrawlSeedDirectory}}
	missing := "no-such-collection"

	for _, c := range []struct {
		name string
		in   services.CrawlInput
		want string
	}{
		{"research", services.CrawlInput{Purpose: "research", Seeds: seed, MaxPages: 5, MaxDepth: 1}, "网页研究批次必须通过研究流程创建"},
		{"collection", services.CrawlInput{Purpose: "documents", Seeds: seed, MaxPages: 5, MaxDepth: 1, CollectionID: &missing}, "目标集合不存在，请重新选择"},
		{"no seeds", services.CrawlInput{Purpose: "documents", MaxPages: 5, MaxDepth: 1}, "请提供 1–10 个入口"},
		{"pages", services.CrawlInput{Purpose: "documents", Seeds: seed, MaxPages: 301, MaxDepth: 1}, "页数须为 1–300，深度须为 0–5"},
		{"depth", services.CrawlInput{Purpose: "documents", Seeds: seed, MaxPages: 5, MaxDepth: 6}, "页数须为 1–300，深度须为 0–5"},
	} {
		if id, err := svc.CreateJob(ctx, c.in); err == nil || err.Error() != c.want || id != "" {
			t.Errorf("%s：id=%q err=%v，期望 %q", c.name, id, err, c.want)
		}
	}
}

// 线路上的异常形状不让整个列表解不出来：合集 id 不是字符串按「未归合集」、error 与 counts 为 null、
// { ok:false } 没给原因时带上频道名、信封不是对象时带频道名、create 没有返回 id 报错。
func TestNativeImportsDocSiteDecodesOddPayloads(t *testing.T) {
	ctx := t.Context()
	b := newSystemBridge()
	reply := func(payload string) handler {
		return func(context.Context, []json.RawMessage) (any, error) { return json.RawMessage(payload), nil }
	}
	b.handlers["crawl:list"] = reply(`{"ok":true,"data":[
		{"id":"j1","input":{"purpose":"documents","seeds":[{"url":"https://docs.example.com/a/","mode":"directory","directory":null}],"collectionId":7,"maxPages":9,"maxDepth":1,"duplicatePolicy":"update"},"status":"paused","createdAt":1,"updatedAt":2,"error":null,"counts":null},
		{"id":"j2","input":{"purpose":"documents","seeds":[],"collectionId":"c9","maxPages":1,"maxDepth":0,"duplicatePolicy":"skip"},"status":"completed","createdAt":3,"updatedAt":4,"error":"robots.txt unavailable","counts":{"added":2}}]}`)
	b.handlers["crawl:get"] = reply(`{"ok":true,"data":{"job":null,"pages":null}}`)
	b.handlers["crawl:pause"] = reply(`{"ok":false}`)
	b.handlers["crawl:cancel"] = reply(`"not an envelope"`)
	b.handlers["crawl:create"] = reply(`{"ok":true}`)
	svc := docSiteAdapter{b: b}

	jobs, err := svc.ListJobs(ctx)
	if err != nil || len(jobs) != 2 {
		t.Fatalf("列表：%+v %v", jobs, err)
	}
	if jobs[0].Input.CollectionID != nil || jobs[0].Error != "" || jobs[0].Counts == nil || jobs[0].Input.Seeds[0].Directory != "" || jobs[0].Input.DuplicatePolicy != "update" {
		t.Errorf("第一条：%+v", jobs[0])
	}
	if jobs[1].Input.CollectionID == nil || *jobs[1].Input.CollectionID != "c9" || jobs[1].Error != "robots.txt unavailable" || jobs[1].Counts["added"] != 2 {
		t.Errorf("第二条：%+v", jobs[1])
	}
	if detail, err := svc.GetJob(ctx, "j1"); err != nil || detail.Job != nil || detail.Pages == nil {
		t.Errorf("job 为 null：%+v %v", detail, err)
	}
	if err := svc.PauseJob(ctx, "j1"); err == nil || !strings.Contains(err.Error(), "crawl:pause") {
		t.Errorf("没有原因的失败应带频道名：%v", err)
	}
	if err := svc.CancelJob(ctx, "j1"); err == nil || !strings.Contains(err.Error(), "crawl:cancel") {
		t.Errorf("信封不是对象：%v", err)
	}
	if _, err := svc.CreateJob(ctx, services.CrawlInput{}); err == nil || !strings.Contains(err.Error(), "批次 id") {
		t.Errorf("create 没有返回 id：%v", err)
	}
	// 没登记的频道：错误直接来自 Bridge，不被吞掉。
	if err := svc.ResumeJob(ctx, "j1"); err == nil || !strings.Contains(err.Error(), "crawl:resume") {
		t.Errorf("没登记的频道：%v", err)
	}
}

// 创建请求的形状与渲染层 webCapture.create 一致：directory 为空时不带、合集为空是 null、重复策略与上限原样给出。
func TestNativeImportsDocSiteCreateRequestShape(t *testing.T) {
	b := newSystemBridge()
	var got []map[string]any
	b.handlers["crawl:create"] = func(_ context.Context, a []json.RawMessage) (any, error) {
		var arg map[string]any
		if len(a) != 1 || json.Unmarshal(a[0], &arg) != nil {
			return nil, errors.New("unexpected args")
		}
		got = append(got, arg)
		return map[string]any{"ok": true, "data": "job-x"}, nil
	}
	svc := docSiteAdapter{b: b}
	col := "c1"
	if id, err := svc.CreateJob(t.Context(), services.CrawlInput{
		Purpose: "documents", MaxPages: 30, MaxDepth: 3, DuplicatePolicy: services.CrawlDuplicateUpdate, CollectionID: &col,
		Seeds: []services.CrawlSeed{{URL: "https://docs.example.com/guide/", Mode: services.CrawlSeedDirectory, Directory: "/guide/"}},
	}); err != nil || id != "job-x" {
		t.Fatalf("创建：%q %v", id, err)
	}
	if _, err := svc.CreateJob(t.Context(), services.CrawlInput{
		Purpose: "documents", MaxPages: 50, MaxDepth: 2, DuplicatePolicy: services.CrawlDuplicateSkip,
		Seeds: []services.CrawlSeed{{URL: "https://docs.example.com/guide/", Mode: services.CrawlSeedDirectory}},
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("收到 %d 次请求", len(got))
	}
	first, second := got[0], got[1]
	seed := func(m map[string]any) map[string]any { return m["seeds"].([]any)[0].(map[string]any) }
	if first["purpose"] != "documents" || first["collectionId"] != "c1" || first["duplicatePolicy"] != "update" || first["maxPages"] != float64(30) || first["maxDepth"] != float64(3) {
		t.Errorf("第一次请求：%v", first)
	}
	if seed(first)["directory"] != "/guide/" || seed(first)["mode"] != "directory" {
		t.Errorf("第一次的入口：%v", seed(first))
	}
	if v, has := second["collectionId"]; !has || v != nil {
		t.Errorf("未归合集应是 collectionId: null：%v", second)
	}
	if _, has := seed(second)["directory"]; has {
		t.Errorf("目录为空时不应带 directory：%v", seed(second))
	}
}

// 平台账号：状态来自真实的会话管理，没有 Cookie 或令牌字段；
// 登录请求的形状是 { platform, forceRelogin, verificationUrl? }，验证地址只在非空时带；
// 没有进行中的登录时取消是 false；取消进行中的 NodeSeek 验证经真实处理器触发取消函数并返回 true。
func TestNativeImportsPlatformAuthAgainstRealHandlers(t *testing.T) {
	ud := t.TempDir()
	l := &Library{paths: Paths{UserData: ud}, configDir: filepath.Join(ud, "config")}
	l.platformCap = newPlatformSessions(l, ud)
	b := newSystemBridge()
	l.registerPlatformCapture(b)
	var loginArgs []map[string]any
	b.handlers["platformCapture:login"] = func(_ context.Context, a []json.RawMessage) (any, error) {
		// 不真的拉起浏览器：只记录请求的形状，回一份已登录的状态
		var arg map[string]any
		if len(a) != 1 || json.Unmarshal(a[0], &arg) != nil {
			return nil, errors.New("unexpected args")
		}
		loginArgs = append(loginArgs, arg)
		return map[string]any{"platform": arg["platform"], "browser": "embedded", "browserVersion": "126.0", "available": true, "loggedIn": true, "busy": false}, nil
	}
	svc := newNativeServices(b, nil).PlatformAuth
	if svc == nil {
		t.Fatal("newNativeServices 应装配 PlatformAuth")
	}
	ctx := t.Context()

	list, err := svc.Statuses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	node, ok := services.FindPlatformStatus(list, services.PlatformNodeseek)
	if !ok || node.LoggedIn || node.Busy || !node.Available || node.Browser != "embedded" {
		t.Fatalf("NodeSeek 的初始状态：%+v（%+v）", node, list)
	}

	target := "https://www.nodeseek.com/post-12345-2"
	st, err := svc.Login(ctx, services.PlatformNodeseek, services.PlatformLoginOptions{VerificationURL: target})
	if err != nil || !st.LoggedIn || st.BrowserVersion != "126.0" {
		t.Fatalf("验证登录：%+v %v", st, err)
	}
	if _, err := svc.Login(ctx, services.PlatformDouyin, services.PlatformLoginOptions{ForceRelogin: true}); err != nil {
		t.Fatal(err)
	}
	if len(loginArgs) != 2 {
		t.Fatalf("登录请求数：%v", loginArgs)
	}
	if loginArgs[0]["platform"] != "nodeseek" || loginArgs[0]["forceRelogin"] != false || loginArgs[0]["verificationUrl"] != target {
		t.Errorf("NodeSeek 登录请求：%v", loginArgs[0])
	}
	if _, has := loginArgs[1]["verificationUrl"]; has || loginArgs[1]["forceRelogin"] != true {
		t.Errorf("普通登录不应带验证地址：%v", loginArgs[1])
	}

	if ok, err := svc.CancelLogin(ctx, services.PlatformNodeseek); err != nil || ok {
		t.Errorf("没有进行中的验证时取消应是 false：%v %v", ok, err)
	}
	canceled := make(chan struct{}, 1)
	l.platformCap.mu.Lock()
	l.platformCap.nodeseekCancel = func() { canceled <- struct{}{} }
	l.platformCap.mu.Unlock()
	if ok, err := svc.CancelLogin(ctx, services.PlatformNodeseek); err != nil || !ok {
		t.Errorf("取消进行中的 NodeSeek 验证：%v %v", ok, err)
	}
	select {
	case <-canceled:
	default:
		t.Error("取消应触发验证的取消函数")
	}
	if _, err := svc.CancelLogin(ctx, "not-a-platform"); err == nil || err.Error() == "" {
		t.Errorf("未知平台应带原因报错：%v", err)
	}
}

// 装配：newNativeServices 把两个适配器都接上；Bridge 里没登记处理器时，错误指明是哪个频道。
func TestNativeImportsServicesWiring(t *testing.T) {
	s := newNativeServices(newSystemBridge(), newPushHub())
	if s.DocSite == nil || s.PlatformAuth == nil {
		t.Fatalf("DocSite 与 PlatformAuth 都应装配：%+v %+v", s.DocSite, s.PlatformAuth)
	}
	if _, err := s.DocSite.ListJobs(t.Context()); err == nil || !strings.Contains(err.Error(), "crawl:list") {
		t.Errorf("没登记处理器时应报「频道未实现」：%v", err)
	}
}
