package main

// 主题阅读运行时：任务排队与执行。对应 services/themed-reading/runtime.ts。
//
// 生成管线本体（编辑稿 / 设计 / 查证 / 运行检查修复）是 TS 原文件在 goja 里运行（themed_pipeline.go），
// 这里负责它之外的一切：前置校验、版本与任务的建立、检查点保存、素材（原图补采 / 主题生图 / 完整性核验）、
// 发布与孤儿素材清理。版本与任务都以有序对象（jsObj）流转，字段顺序与 TS 一致。

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"sync"
	"time"
)

const (
	themedStopReason = "用户已停止主题排版"
)

func themedNow() float64 { return float64(time.Now().UnixMilli()) }

func themedClone(v any) any {
	s, ok := jsStringify(v)
	if !ok {
		return jsUndefined
	}
	out, err := jsParse(s)
	if err != nil {
		return jsUndefined
	}
	return out
}

func themedObj(v any) *jsObj { o, _ := v.(*jsObj); return o }
func themedArr(v any) jsArr  { a, _ := v.(jsArr); return a }
func themedNum(v any) float64 {
	f, _ := v.(float64)
	return f
}
func themedBool(v any) bool { b, _ := v.(bool); return b }

type themedRuntime struct {
	l         *Library
	pages     *themedPages
	mu        sync.Mutex
	preparing map[string]bool
	activeID  string
	cancelRun context.CancelCauseFunc
	pumping   bool
	stopped   bool
	blocked   bool
	wg        sync.WaitGroup
}

func (l *Library) newThemedRuntime() *themedRuntime {
	rt := &themedRuntime{l: l, pages: l.themedPages(), preparing: map[string]bool{}}
	l.themedRT = rt
	l.themedAbort = rt.abortActive
	return rt
}

func (rt *themedRuntime) abortActive(id string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.activeID == id && rt.cancelRun != nil {
		rt.cancelRun(errors.New(themedStopReason))
	}
}

// hasActive 对应 hasActive：有任务在准备 / 排队 / 执行。
func (rt *themedRuntime) hasActive(ctx context.Context) bool {
	rt.mu.Lock()
	local := len(rt.preparing) > 0 || rt.activeID != ""
	rt.mu.Unlock()
	if local {
		return true
	}
	active, _ := rt.pages.hasActiveTasks(ctx)
	return active
}

// block 对应 block：数据目录切换 / 恢复备份前拒绝仍有任务的状态，并阻止新任务。
func (rt *themedRuntime) block(ctx context.Context) (func(), error) {
	if rt.hasActive(ctx) {
		return nil, errors.New("主题排版正在执行，请先停止任务后再操作数据目录或恢复备份")
	}
	rt.mu.Lock()
	rt.blocked = true
	rt.mu.Unlock()
	return func() { rt.mu.Lock(); rt.blocked = false; rt.mu.Unlock() }, nil
}

func (rt *themedRuntime) retireIdle(ctx context.Context) error {
	if _, err := rt.block(ctx); err != nil {
		return err
	}
	rt.mu.Lock()
	rt.stopped = true
	rt.mu.Unlock()
	return nil
}

func (rt *themedRuntime) isStoppedOrBlocked() bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.stopped || rt.blocked
}

// saveTask 对应 runtime.saveTask：写入更新时间、保存并通知（通知的是内存里的任务对象）。
func (rt *themedRuntime) saveTask(ctx context.Context, task *jsObj) error {
	task.set("updatedAt", themedNow())
	if _, err := rt.pages.saveTask(ctx, task); err != nil {
		return err
	}
	rt.l.themedNotify(task)
	return nil
}

func (rt *themedRuntime) assertCurrent(ctx context.Context, task *jsObj) error {
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	live, err := rt.pages.getTask(ctx, themedStr(task, "id"))
	if err != nil {
		return err
	}
	version, err := rt.pages.getVersion(ctx, themedStr(task, "versionId"))
	if err != nil {
		return err
	}
	item, err := rt.l.themedItem(ctx, themedStr(task, "itemId"))
	if err != nil {
		return err
	}
	if live == nil || !themedActive(themedStr(live, "state")) || version == nil || themedStr(version, "role") != "working" || item == nil || item.DeletedAt != nil {
		return errors.New("任务已取消、被替代，或条目已删除，结果未写入")
	}
	return nil
}

func (rt *themedRuntime) activeTaskFor(ctx context.Context, itemID, kind, exceptID string) (bool, error) {
	tasks, err := rt.pages.listTasks(ctx)
	if err != nil {
		return false, err
	}
	for _, t := range tasks {
		if themedStr(t, "id") != exceptID && themedStr(t, "itemId") == itemID && themedStr(t, "sourceKind") == kind && themedActive(themedStr(t, "state")) {
			return true, nil
		}
	}
	return false, nil
}

// buildSource 对应 buildThemedReadingSource。
func (l *Library) themedBuildSource(ctx context.Context, item *themedItem, kind string) (*jsObj, error) {
	content, err := themedReadingContent(item, kind)
	if err != nil {
		return nil, err
	}
	blocksJSON, err := themedCall("renderBlocks", content)
	if err != nil {
		return nil, err
	}
	blocks, err := jsParse(blocksJSON)
	if err != nil {
		return nil, err
	}
	var uri any = jsNull
	if item.SourceURI != nil {
		uri = *item.SourceURI
	}
	src := newObj()
	src.set("title", item.Title)
	src.set("content", content)
	src.set("sourceUri", uri)
	src.set("fingerprint", themedFingerprint(item.Title, content, item.SourceURI, kind))
	src.set("blocks", blocks)
	return src, nil
}

type themedGenerateInput struct {
	itemID, kind string
	options      *jsObj
}

// generate 对应 ThemedReadingRuntime.generate。
func (rt *themedRuntime) generate(ctx context.Context, in themedGenerateInput, assetID string, reuseDesign bool) (*jsObj, error) {
	l := rt.l
	if rt.isStoppedOrBlocked() {
		return nil, errors.New("数据服务正在切换，请稍后重试")
	}
	key := in.itemID + ":" + in.kind
	rt.mu.Lock()
	busy := rt.preparing[key]
	rt.mu.Unlock()
	if !busy {
		var err error
		if busy, err = rt.activeTaskFor(ctx, in.itemID, in.kind, ""); err != nil {
			return nil, err
		}
	}
	if busy {
		return nil, errors.New("这份内容已有主题排版任务")
	}
	opts := in.options
	if l.resolveMediaSummaryConfig() == nil && assetID == "" && !reuseDesign {
		return nil, errors.New("请先配置可用的主文本模型")
	}
	generateImages := themedBool(opts.get("generateImages"))
	if generateImages && themedNum(opts.get("maxImages")) < 1 {
		return nil, errors.New("已开启生成主题图片，请将图片上限设为至少一张")
	}
	if generateImages && l.resolveImageGenConfig() == nil {
		return nil, errors.New("未配置可用的生图模型，请关闭配图或前往设置")
	}
	rt.mu.Lock()
	rt.preparing[key] = true
	rt.mu.Unlock()
	defer func() { rt.mu.Lock(); delete(rt.preparing, key); rt.mu.Unlock() }()

	item, err := l.themedItem(ctx, in.itemID)
	if err != nil {
		return nil, err
	}
	if item == nil || item.DeletedAt != nil {
		return nil, errors.New("条目不存在或已移入回收站")
	}
	current, err := rt.pages.get(ctx, in.itemID, in.kind, "")
	if err != nil {
		return nil, err
	}
	action, _ := opts.get("action").(string)
	useCurrent := assetID != "" || reuseDesign || action == "revise" || action == "redesign" || (themedBool(opts.get("fromCurrent")) && action != "refresh")
	var existing *jsObj
	if useCurrent {
		existing = current
	}
	if useCurrent && existing == nil {
		return nil, errors.New("还没有可调整的主题页")
	}
	if existing != nil && themedNum(existing.get("formatVersion")) < 3 {
		return nil, errors.New(themedLegacyPageError)
	}
	var source *jsObj
	if existing != nil {
		source = themedObj(themedClone(existing.get("source")))
	} else if source, err = l.themedBuildSource(ctx, item, in.kind); err != nil {
		return nil, err
	}
	if len(themedArr(source.get("blocks"))) == 0 {
		return nil, errors.New("当前内容为空，没有可排版的正文")
	}
	now := themedNow()
	reconstruction := func() *jsObj {
		if (assetID != "" || reuseDesign || action == "redesign" || action == "revise") && existing != nil {
			return themedObj(themedClone(existing.get("reconstruction")))
		}
		r := newObj()
		for _, k := range []string{"notes", "queries", "references", "draft", "interactions"} {
			r.set(k, jsArr{})
		}
		return r
	}()
	version := newObj()
	version.set("id", themedUUID())
	version.set("itemId", item.ID)
	version.set("sourceKind", in.kind)
	version.set("role", "working")
	version.set("formatVersion", float64(3))
	version.set("source", source)
	version.set("reconstruction", reconstruction)
	vopts := newObj()
	spreadInto(vopts, opts)
	if opts.get("research") == jsUndefined || opts.get("research") == jsNull {
		vopts.set("research", false)
	}
	version.set("options", vopts)
	if (assetID != "" || reuseDesign) && existing != nil {
		version.set("design", themedClone(existing.get("design")))
	} else {
		version.set("design", jsNull)
	}
	if (assetID != "" || reuseDesign) && existing != nil {
		version.set("designDirection", existing.get("designDirection"))
	} else {
		version.set("designDirection", jsUndefined)
	}
	if existing != nil {
		version.set("textModel", existing.get("textModel"))
		version.set("imageModel", existing.get("imageModel"))
		version.set("assets", themedClone(existing.get("assets")))
	} else {
		version.set("textModel", jsUndefined)
		version.set("imageModel", jsUndefined)
		version.set("assets", jsArr{})
	}
	version.set("warnings", jsArr{})
	version.set("createdAt", now)
	version.set("updatedAt", now)
	recon := reconstruction
	if assetID == "" && !reuseDesign && action == "revise" {
		recon.set("revisionDraft", themedClone(recon.get("draft")))
		recon.set("draft", jsArr{})
		recon.set("outline", jsUndefined)
		recon.set("queries", jsArr{})
		recon.set("selectedUrls", jsUndefined)
		recon.set("researchComplete", false)
		recon.set("researchBatches", jsUndefined)
		recon.set("researchReview", jsUndefined)
		recon.set("readUrls", jsUndefined)
	}
	if assetID == "" && !reuseDesign && (action == "redesign" || action == "revise") {
		recon.set("interactions", jsArr{})
		recon.set("visuals", jsArr{})
		recon.set("animations", jsArr{})
	}
	if existing == nil {
		assetsJSON, err := themedCall("collectOriginalAssets", themedJSON(version))
		if err != nil {
			return nil, err
		}
		assets, err := jsParse(assetsJSON)
		if err != nil {
			return nil, err
		}
		version.set("assets", assets)
	}
	if reuseDesign && existing != nil {
		design := themedObj(existing.get("design"))
		failedScript := false
		if design != nil {
			for _, s := range themedArr(design.get("scripts")) {
				if themedStr(s, "status") == "failed" {
					failedScript = true
				}
			}
		}
		if failedScript && vTruthy(existing.get("generation")) {
			gen := themedObj(themedClone(existing.get("generation")))
			gen.set("repairs", newObj())
			version.set("generation", gen)
			version.set("design", jsNull)
			for _, s := range themedArr(gen.get("scripts")) {
				if so := themedObj(s); so != nil {
					so.set("status", "pending")
				}
			}
		}
	}
	if assetID != "" {
		var asset *jsObj
		for _, a := range themedArr(version.get("assets")) {
			if themedStr(a, "id") == assetID {
				asset = themedObj(a)
			}
		}
		if asset == nil {
			return nil, errors.New("图片槽位不存在")
		}
		if themedStr(asset, "role") == "generated" && l.resolveImageGenConfig() == nil {
			return nil, errors.New("请先配置生图模型")
		}
		asset.set("status", "pending")
		asset.delete("error")
	}
	if rt.isStoppedOrBlocked() {
		return nil, errors.New("数据服务正在切换")
	}
	oldAssets, err := rt.pages.listAssetFiles(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := rt.pages.saveVersion(ctx, version, nil); err != nil {
		return nil, err
	}
	readyCount := func(list jsArr, pred func(any) bool) float64 {
		n := 0.0
		for _, x := range list {
			if pred(x) {
				n++
			}
		}
		return n
	}
	reused := newObj()
	reused.set("chapters", float64(len(themedArr(recon.get("draft")))))
	reused.set("notes", float64(len(themedArr(recon.get("notes")))))
	reused.set("references", readyCount(themedArr(recon.get("references")), func(r any) bool { return themedStr(r, "status") == "ready" }))
	reused.set("assets", readyCount(themedArr(version.get("assets")), func(a any) bool { return themedStr(a, "status") == "ready" }))
	usage := newObj()
	for _, k := range []string{"textCalls", "imageCalls", "imagesSaved", "searchCalls", "pagesRead"} {
		usage.set(k, float64(0))
	}
	task := newObj()
	task.set("id", themedUUID())
	task.set("itemId", item.ID)
	task.set("sourceKind", in.kind)
	task.set("versionId", themedStr(version, "id"))
	task.set("reused", reused)
	task.set("title", themedStr(source, "title"))
	task.set("state", "queued")
	task.set("stage", "prepare")
	task.set("completed", float64(0))
	task.set("total", float64(0))
	if assetID != "" {
		task.set("assetId", assetID)
	} else {
		task.set("assetId", jsUndefined)
	}
	task.set("usage", usage)
	task.set("createdAt", now)
	task.set("updatedAt", now)
	if err := rt.saveTask(ctx, task); err != nil {
		return nil, err
	}
	_, _ = l.cleanupOrphanAssets(ctx, oldAssets)
	rt.kick()
	return task, nil
}

// cancel 对应 runtime.cancel（与 IPC 层共用 themedCancel 的持久化与通知）。
func (rt *themedRuntime) cancel(ctx context.Context, id string) (*jsObj, error) {
	return rt.l.themedCancel(ctx, id)
}

func (rt *themedRuntime) continueOffline(ctx context.Context, id string) (*jsObj, error) {
	task, err := rt.pages.getTask(ctx, id)
	if err != nil {
		return nil, err
	}
	var version *jsObj
	if task != nil {
		if version, err = rt.pages.getVersion(ctx, themedStr(task, "versionId")); err != nil {
			return nil, err
		}
	}
	if version != nil && themedNum(version.get("formatVersion")) < 3 {
		return nil, errors.New(themedLegacyTaskError)
	}
	if task == nil || version == nil || themedStr(version, "role") != "working" || themedStr(task, "stage") != "research" || themedActive(themedStr(task, "state")) {
		return nil, errors.New("当前任务不能改为不联网继续")
	}
	themedObj(version.get("options")).set("research", false)
	if _, err := rt.pages.saveVersion(ctx, version, nil); err != nil {
		return nil, err
	}
	return rt.resume(ctx, id)
}

var reThemedLimit = regexp.MustCompile(`查证上限`)

// resume 对应 runtime.resume。
func (rt *themedRuntime) resume(ctx context.Context, id string) (*jsObj, error) {
	if rt.isStoppedOrBlocked() {
		return nil, errors.New("数据服务正在切换")
	}
	task, err := rt.pages.getTask(ctx, id)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, errors.New("任务不存在")
	}
	if themedActive(themedStr(task, "state")) {
		return task, nil
	}
	version, err := rt.pages.getVersion(ctx, themedStr(task, "versionId"))
	if err != nil {
		return nil, err
	}
	item, err := rt.l.themedItem(ctx, themedStr(task, "itemId"))
	if err != nil {
		return nil, err
	}
	if version == nil || item == nil || item.DeletedAt != nil {
		return nil, errors.New("任务内容已删除，无法继续")
	}
	if themedNum(version.get("formatVersion")) < 3 {
		if themedStr(version, "role") == "working" {
			return nil, errors.New(themedLegacyTaskError)
		}
		return nil, errors.New(themedLegacyPageError)
	}
	opts := themedObj(version.get("options"))
	if themedStr(version, "role") != "working" {
		if themedStr(version, "role") != "current" {
			return nil, errors.New("页面已有新版本，请在当前主题页调整")
		}
		failed := false
		for _, a := range themedArr(version.get("assets")) {
			if themedStr(a, "status") != "ready" {
				failed = true
			}
		}
		design := themedObj(version.get("design"))
		if design != nil {
			for _, r := range themedArr(design.get("visualResults")) {
				if themedStr(r, "status") == "failed" {
					failed = true
				}
			}
			for _, s := range themedArr(design.get("scripts")) {
				if themedStr(s, "status") == "failed" {
					failed = true
				}
			}
		}
		if !failed {
			return nil, errors.New("这份页面没有需要继续的素材")
		}
		o := newObj()
		spreadInto(o, opts)
		o.set("fromCurrent", true)
		gen := false
		for _, a := range themedArr(version.get("assets")) {
			if themedStr(a, "role") == "generated" && themedStr(a, "status") != "ready" {
				gen = true
			}
		}
		o.set("generateImages", gen)
		return rt.generate(ctx, themedGenerateInput{themedStr(task, "itemId"), themedStr(task, "sourceKind"), o}, "", true)
	}
	if other, err := rt.activeTaskFor(ctx, themedStr(task, "itemId"), themedStr(task, "sourceKind"), id); err != nil {
		return nil, err
	} else if other {
		return nil, errors.New("已有其他活动任务")
	}
	recon := themedObj(version.get("reconstruction"))
	if themedStr(task, "stage") == "research" && themedBool(opts.get("research")) {
		out, err := themedCall("prepareResearchRetry", themedJSON(recon))
		if err != nil {
			return nil, err
		}
		parsed, _ := jsParse(out)
		version.set("reconstruction", parsed)
	}
	gen := themedObj(version.get("generation"))
	if gen != nil && themedStr(task, "stage") == "research" && reThemedLimit.MatchString(themedStr(task, "error")) {
		limit := gen.get("researchRoundLimit")
		base := 1.0
		if themedStr(opts, "researchDepth") == "deep" {
			base = 2
		}
		if f, ok := limit.(float64); ok {
			base = f
		}
		gen.set("researchRoundLimit", base+1)
	}
	if gen != nil {
		gen.set("repairs", newObj())
		for _, m := range themedArr(gen.get("scripts")) {
			if mo := themedObj(m); mo != nil && themedStr(mo, "status") == "failed" {
				mo.set("status", "pending")
			}
		}
	}
	for _, a := range themedArr(version.get("assets")) {
		if ao := themedObj(a); ao != nil && themedStr(ao, "status") == "failed" {
			ao.set("status", "pending")
			ao.delete("error")
		}
	}
	if _, err := rt.pages.saveVersion(ctx, version, nil); err != nil {
		return nil, err
	}
	task.set("state", "queued")
	task.delete("error")
	if err := rt.saveTask(ctx, task); err != nil {
		return nil, err
	}
	rt.kick()
	return task, nil
}

// cancelForItems 对应 cancelThemedReadingForItems：取消这些条目仍在排队 / 执行的任务（失败只记日志，不阻断删除）。
func (rt *themedRuntime) cancelForItems(itemIDs []string) {
	ids := map[string]bool{}
	for _, id := range itemIDs {
		ids[id] = true
	}
	ctx := context.Background()
	tasks, err := rt.pages.listTasks(ctx)
	if err != nil {
		rt.l.paths.logAppError("themedReading", "cancelForItems", err.Error(), nil)
		return
	}
	for _, t := range tasks {
		if ids[themedStr(t, "itemId")] && themedActive(themedStr(t, "state")) {
			if _, err := rt.cancel(ctx, themedStr(t, "id")); err != nil {
				rt.l.paths.logAppError("themedReading", "cancelForItems", err.Error(), nil)
			}
		}
	}
}

// kick 对应 kick：没有执行线程时启动一个，处理完再检查是否还有排队任务。
func (rt *themedRuntime) kick() {
	rt.mu.Lock()
	if rt.pumping || rt.stopped {
		rt.mu.Unlock()
		return
	}
	rt.pumping = true
	rt.wg.Add(1)
	rt.mu.Unlock()
	go func() {
		defer rt.wg.Done()
		rt.pump()
		rt.mu.Lock()
		rt.pumping = false
		stopped := rt.stopped
		rt.mu.Unlock()
		if stopped {
			return
		}
		tasks, _ := rt.pages.listTasks(context.Background())
		for _, t := range tasks {
			if themedStr(t, "state") == "queued" {
				rt.kick()
				return
			}
		}
	}()
}

func (rt *themedRuntime) pump() {
	bg := context.Background()
	for !rt.isStopped() {
		tasks, err := rt.pages.listTasks(bg)
		if err != nil {
			rt.l.paths.logAppError("themedReading", "generate", err.Error(), nil)
			return
		}
		var queued []*jsObj
		for _, t := range tasks {
			if themedStr(t, "state") == "queued" {
				queued = append(queued, t)
			}
		}
		if len(queued) == 0 {
			return
		}
		sort.SliceStable(queued, func(i, j int) bool {
			return themedNum(queued[i].get("createdAt")) < themedNum(queued[j].get("createdAt"))
		})
		task := queued[0]
		ctx, cancel := context.WithCancelCause(bg)
		rt.mu.Lock()
		rt.activeID, rt.cancelRun = themedStr(task, "id"), cancel
		rt.mu.Unlock()
		err = rt.execute(ctx, task)
		aborted := ctx.Err() != nil
		if err != nil {
			live, _ := rt.pages.getTask(bg, themedStr(task, "id"))
			// 用户可能已将取消的任务重新入队，旧请求的取消回执不能覆盖新一轮。
			if live != nil && themedActive(themedStr(live, "state")) && !aborted {
				state := "failed"
				if rt.isStopped() {
					state = "interrupted"
				}
				live.set("state", state)
				live.set("error", describeThemeError(err))
				_ = rt.saveTask(bg, live)
			}
			if !aborted {
				extra := newObj()
				extra.set("itemId", themedStr(task, "itemId"))
				rt.l.paths.logAppError("themedReading", "generate", describeThemeError(err), extra)
			}
		}
		cancel(nil)
		rt.mu.Lock()
		rt.activeID, rt.cancelRun = "", nil
		rt.mu.Unlock()
	}
}

func (rt *themedRuntime) isStopped() bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.stopped
}

func describeThemeError(err error) string { return err.Error() }

// shutdown 对应 runtime.shutdown：把活动任务标为已中断并等待执行线程退出。
func (rt *themedRuntime) shutdown(ctx context.Context) {
	rt.mu.Lock()
	rt.stopped = true
	cancel := rt.cancelRun
	rt.mu.Unlock()
	tasks, _ := rt.pages.listTasks(ctx)
	for _, t := range tasks {
		if themedActive(themedStr(t, "state")) {
			t.set("state", "interrupted")
			t.set("error", "应用已关闭，手动继续可复用已完成结果")
			_ = rt.saveTask(ctx, t)
		}
	}
	if cancel != nil {
		cancel(errors.New("应用正在关闭"))
	}
	rt.wg.Wait()
}
