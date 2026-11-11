package main

// 主题阅读运行时：单个任务的执行流程。对应 runtime.ts 的 execute。
//
// 版本以有序对象在 Go 里流转；管线（设计 / 运行检查修复）在 goja 里改的是它的副本，
// 每次检查点把最新快照整体换回来，再沿用 TS 的保存 / 通知语义。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

func themedAssign(dst, src *jsObj) {
	for _, k := range src.keys() {
		dst.set(k, src.get(k))
	}
}

// themedSame 是 JS 的 !==（仅数值 / 字符串 / 缺省三种情形）。
func themedSame(a, b any) bool {
	undef := func(v any) bool { return v == nil || v == jsUndefined || v == jsNull }
	if undef(a) || undef(b) {
		return undef(a) && undef(b)
	}
	return a == b
}

func (rt *themedRuntime) pipelineServices() pipelineServices {
	l := rt.l
	chat, search := aiChatFunc(l.chatCompletion), l.searchReadingWeb
	if l.chatFn != nil {
		chat = l.chatFn
	}
	if l.searchFn != nil {
		search = l.searchFn
	}
	return pipelineServices{
		Chat:   chat,
		Search: search,
		Capture: func(ctx context.Context, req webCaptureRequest) (webCaptureResult, error) {
			res := l.captureWebPage(ctx, req, nil)
			if res.release != nil {
				defer res.release()
			}
			if ctx.Err() != nil {
				return res, context.Cause(ctx)
			}
			return res, nil
		},
		Usage: func(scenario, model string, prompt, completion *float64, failed bool) {
			u := mainAIUsage{Scenario: scenario, Model: model, Failed: failed}
			if prompt != nil {
				u.PromptTokens = *prompt
			}
			if completion != nil {
				u.CompletionTokens = *completion
			}
			l.recordMainAIUsage(context.Background(), u)
		},
		Verify: l.themedVerify,
	}
}

func (rt *themedRuntime) execute(ctx context.Context, task *jsObj) error {
	l := rt.l
	pages := rt.pages
	version, err := pages.getVersion(ctx, themedStr(task, "versionId"))
	if err != nil {
		return err
	}
	if version == nil {
		return errors.New("工作版本不存在")
	}
	if themedNum(version.get("formatVersion")) < 3 {
		return errors.New(themedLegacyTaskError)
	}
	task.set("state", "running")
	if err := rt.saveTask(ctx, task); err != nil {
		return err
	}
	if err := rt.assertCurrent(ctx, task); err != nil {
		return err
	}
	designOf := func() *jsObj { return themedObj(version.get("design")) }

	checkpoint := func() error {
		if err := rt.assertCurrent(ctx, task); err != nil {
			return err
		}
		version.set("updatedAt", themedNow())
		if gen := themedObj(version.get("generation")); gen != nil {
			task.set("issues", gen.get("issues"))
			if !themedSame(gen.get("revision"), task.get("previewRevision")) {
				task.set("previewRevision", gen.get("revision"))
				if reused := themedObj(task.get("reused")); reused != nil {
					c := themedNum(reused.get("chapters"))
					if r := themedNum(gen.get("reusedChapters")); r > c {
						c = r
					}
					reused.set("chapters", c)
				}
				if len(themedArr(gen.get("sections"))) > 0 && themedSame(task.get("firstContentAt"), nil) {
					task.set("firstContentAt", themedNow())
				}
			}
		}
		before, err := pages.getVersion(ctx, themedStr(version, "id"))
		if err != nil {
			return err
		}
		saved := 0
		prev := map[string]string{}
		if before != nil {
			for _, a := range themedArr(before.get("assets")) {
				prev[themedStr(a, "id")] = themedStr(a, "status")
			}
		}
		for _, a := range themedArr(version.get("assets")) {
			if themedStr(a, "role") == "generated" && themedStr(a, "status") == "ready" && prev[themedStr(a, "id")] != "ready" {
				saved++
			}
		}
		if usage := themedObj(task.get("usage")); saved > 0 && usage != nil {
			next := themedCopyObj(task)
			nu := themedCopyObj(usage)
			nu.set("imagesSaved", themedNum(usage.get("imagesSaved"))+float64(saved))
			next.set("usage", nu)
			next.set("updatedAt", themedNow())
			if _, err := pages.saveVersion(ctx, version, next); err != nil {
				return err
			}
			themedAssign(task, next)
			l.themedNotify(task)
			return nil
		}
		if _, err := pages.saveVersion(ctx, version, task); err != nil {
			return err
		}
		l.themedNotify(task)
		return nil
	}
	recordRequest := func(kind string) error {
		if err := rt.assertCurrent(ctx, task); err != nil {
			return err
		}
		// 旧任务没有历史基数，继续时也不把未知历史伪装成零次调用。
		if usage := themedObj(task.get("usage")); usage != nil {
			next := themedCopyObj(task)
			nu := themedCopyObj(usage)
			nu.set(kind, themedNum(usage.get(kind))+1)
			next.set("usage", nu)
			if err := rt.saveTask(ctx, next); err != nil {
				return err
			}
			themedAssign(task, next)
		}
		return nil
	}
	stageStarted := time.Now()
	stage := func(value string, completed, total int) error {
		if err := rt.assertCurrent(ctx, task); err != nil {
			return err
		}
		if themedStr(task, "stage") != value {
			timings := themedObj(task.get("timings"))
			if timings == nil {
				timings = newObj()
				task.set("timings", timings)
			}
			old := themedStr(task, "stage")
			timings.set(old, themedNum(timings.get(old))+float64(time.Since(stageStarted).Milliseconds()))
			stageStarted = time.Now()
		}
		task.set("stage", value)
		task.set("completed", float64(completed))
		task.set("total", float64(total))
		return rt.saveTask(ctx, task)
	}
	// 管线检查点：把 JS 侧最新快照换进来再走 TS 的检查点逻辑。
	adopt := func(snapshot string) error {
		parsed, err := jsParse(snapshot)
		if err != nil {
			return err
		}
		if o := themedObj(parsed); o != nil {
			version = o
		}
		return nil
	}

	text := l.resolveMediaSummaryConfig()
	svc := rt.pipelineServices()
	engine, err := getPipelineEngine()
	if err != nil {
		return err
	}
	cfgJSON := func() string {
		if text == nil {
			return "null"
		}
		b, _ := json.Marshal(map[string]string{"provider": text.Provider, "apiProtocol": text.APIProtocol, "apiKey": text.APIKey, "apiUrl": text.APIURL, "model": text.Model})
		return string(b)
	}
	if designOf() == nil {
		if text == nil {
			return errors.New("主文本模型未配置")
		}
		version.set("textModel", text.Model)
		if err := stage("design", 0, 0); err != nil {
			return err
		}
		lastProgress := time.Time{}
		hooks := pipelineHooks{
			Checkpoint: func(s string) error {
				if err := adopt(s); err != nil {
					return err
				}
				return checkpoint()
			},
			Stage:   func(st string, d, t int) error { return stage(st, d, t) },
			Request: recordRequest,
			DesignProgress: func(attempt, chars int) error {
				if err := rt.assertCurrent(ctx, task); err != nil {
					return err
				}
				if chars != 0 && time.Since(lastProgress) < time.Second {
					return nil
				}
				lastProgress = time.Now()
				dp := newObj()
				dp.set("attempt", float64(attempt))
				dp.set("receivedChars", float64(chars))
				task.set("designProgress", dp)
				return rt.saveTask(ctx, task)
			},
		}
		args := `{"version":` + themedJSON(version) + `,"config":` + cfgJSON() + `}`
		result, err := engine.Run(ctx, "design", args, hooks, svc)
		if err != nil {
			return err
		}
		if err := adopt(result); err != nil {
			return err
		}
		if err := rt.ensureRequestedImage(&version); err != nil {
			return err
		}
		usedJSON, err := themedCall("usedAssetIds", themedStr(designOf(), "html"))
		if err != nil {
			return err
		}
		var used []string
		_ = json.Unmarshal([]byte(usedJSON), &used)
		set := map[string]bool{}
		for _, id := range used {
			set[id] = true
		}
		// 没有进入实际页面的装饰不发起生图，也不计入缺图告警和导出。
		kept := jsArr{}
		for _, a := range themedArr(version.get("assets")) {
			if set[themedStr(a, "id")] {
				kept = append(kept, a)
			}
		}
		version.set("assets", kept)
		if err := checkpoint(); err != nil {
			return err
		}
	}
	opts := themedObj(version.get("options"))
	taskAssetID := themedStr(task, "assetId")
	if taskAssetID == "" && themedBool(opts.get("generateImages")) {
		if err := rt.ensureRequestedImage(&version); err != nil {
			return err
		}
		if err := checkpoint(); err != nil {
			return err
		}
	}
	imageBudget := 5
	if taskAssetID != "" {
		imageBudget = 1
	} else if m := int(themedNum(opts.get("maxImages"))); m < imageBudget {
		imageBudget = m
	}
	var pending []*jsObj
	for _, a := range themedArr(version.get("assets")) {
		asset := themedObj(a)
		if themedStr(asset, "status") == "ready" || (taskAssetID != "" && themedStr(asset, "id") != taskAssetID) {
			continue
		}
		if themedStr(asset, "role") == "original" {
			pending = append(pending, asset)
			continue
		}
		if taskAssetID != "" || themedBool(opts.get("generateImages")) {
			budget := imageBudget
			imageBudget--
			if budget > 0 {
				pending = append(pending, asset)
			}
		}
	}
	imageConfig := l.resolveImageGenConfig()
	planned := 0
	hasGenerated := false
	for _, a := range pending {
		if themedStr(a, "role") == "generated" {
			planned++
			hasGenerated = true
		}
	}
	task.set("plannedImages", float64(planned))
	if err := rt.saveTask(ctx, task); err != nil {
		return err
	}
	if imageConfig != nil && hasGenerated {
		version.set("imageModel", imageConfig.Model)
	}
	for index, asset := range pending {
		if err := stage("images", index, len(pending)); err != nil {
			return err
		}
		err := func() error {
			asset.set("error", "请求正在执行；中断后请检查结果，再手动继续")
			if err := checkpoint(); err != nil {
				return err
			}
			cleanup := func(file string) { _, _ = l.cleanupOrphanAssets(ctx, []string{file}) }
			if themedStr(asset, "role") == "original" {
				return l.prepareOriginalAsset(ctx, asset, checkpoint, cleanup)
			}
			if imageConfig == nil {
				return errors.New("生图模型未配置")
			}
			direction := themedStr(version, "designDirection")
			if direction == "" {
				direction = themedStr(designOf(), "direction")
			}
			return l.generateThemeAsset(ctx, asset, direction, *imageConfig, checkpoint, cleanup, func() error { return recordRequest("imageCalls") })
		}()
		if err != nil {
			if cerr := rt.assertCurrent(ctx, task); cerr != nil {
				return cerr
			}
			asset.set("status", "failed")
			asset.set("error", describeThemeError(err))
			if err := checkpoint(); err != nil {
				return err
			}
			extra := newObj()
			extra.set("itemId", themedStr(task, "itemId"))
			l.paths.logAppError("themedReading", "asset", themedStr(asset, "error"), extra)
		}
		if err := stage("images", index+1, len(pending)); err != nil {
			return err
		}
	}
	if taskAssetID != "" {
		ok := false
		for _, a := range themedArr(version.get("assets")) {
			if themedStr(a, "id") == taskAssetID && themedStr(a, "status") == "ready" {
				ok = true
			}
		}
		if !ok {
			return errors.New("指定图片未能替换，原页面和图片已保留")
		}
	}
	// 说明：compileReadingVisuals 属于已冻结的 reading-graphics 功能；v3 管线只产出 svg 图形，无需编译（见 MYGO-MIGRATION 决定 e）。
	view, err := l.themedInspectImages(version)
	if err != nil {
		return err
	}
	version.set("assets", view.get("assets"))
	if err := rt.assertCurrent(ctx, task); err != nil {
		return err
	}
	warnings := jsArr{}
	for _, a := range themedArr(version.get("assets")) {
		if themedStr(a, "status") != "ready" {
			msg, ok := themedObj(a).get("error").(string)
			if !ok {
				msg = "图片未完成"
			}
			warnings = append(warnings, themedStr(a, "alt")+"："+msg)
		}
	}
	if d := designOf(); d != nil {
		for _, r := range themedArr(d.get("visualResults")) {
			if themedStr(r, "status") == "failed" {
				msg, ok := themedObj(r).get("error").(string)
				if !ok {
					msg = "undefined"
				}
				warnings = append(warnings, "图形 "+themedStr(r, "id")+"："+msg)
			}
		}
	}
	version.set("warnings", warnings)
	if err := stage("assemble", 0, 0); err != nil {
		return err
	}
	if err := checkpoint(); err != nil {
		return err
	}
	if err := stage("validate", 0, 0); err != nil {
		return err
	}
	if _, err := themedCall("document", themedJSON(version), nil, nil); err != nil {
		return err
	}
	if svc.Verify != nil {
		hooks := pipelineHooks{
			Checkpoint: func(s string) error {
				if err := adopt(s); err != nil {
					return err
				}
				return checkpoint()
			},
			Request: func(string) error { return recordRequest("textCalls") },
		}
		args := `{"version":` + themedJSON(version) + `,"config":` + cfgJSON() + `}`
		result, err := engine.Run(ctx, "repair", args, hooks, svc)
		if err != nil {
			return err
		}
		if err := adopt(result); err != nil {
			return err
		}
	}
	if err := rt.assertCurrent(ctx, task); err != nil {
		return err
	}
	warnList := themedArr(version.get("warnings"))
	if d := designOf(); d != nil {
		for _, s := range themedArr(d.get("scripts")) {
			if themedStr(s, "status") == "failed" {
				msg, ok := themedObj(s).get("error").(string)
				if !ok {
					msg = "交互未完成"
				}
				warnList = append(warnList, themedStr(s, "id")+"："+msg)
			}
		}
	}
	version.set("warnings", warnList)
	if err := checkpoint(); err != nil {
		return err
	}
	if err := rt.assertCurrent(ctx, task); err != nil {
		return err
	}
	oldAssets, err := pages.listAssetFiles(ctx)
	if err != nil {
		return err
	}
	completed := themedCopyObj(task)
	timings := newObj()
	if old := themedObj(task.get("timings")); old != nil {
		spreadInto(timings, old)
	}
	cur := themedStr(task, "stage")
	timings.set(cur, themedNum(timings.get(cur))+float64(time.Since(stageStarted).Milliseconds()))
	completed.set("timings", timings)
	state := "completed"
	if len(warnList) > 0 {
		state = "partial"
	}
	completed.set("state", state)
	completed.set("stage", "done")
	parts := make([]string, len(warnList))
	for i, w := range warnList {
		parts[i], _ = w.(string)
	}
	if joined := strings.Join(parts, "；"); joined != "" {
		completed.set("error", joined)
	} else {
		completed.set("error", jsUndefined)
	}
	completed.set("updatedAt", themedNow())
	if _, err := pages.publish(ctx, themedStr(version, "id"), completed); err != nil {
		return err
	}
	themedAssign(task, completed)
	l.themedNotify(task)
	_, _ = l.cleanupOrphanAssets(ctx, oldAssets)
	return nil
}

// ensureRequestedImage 对应 ensureRequestedReadingImage：由 GZT 里的 TS 原实现原地补齐用户指定的图片槽位。
func (rt *themedRuntime) ensureRequestedImage(version **jsObj) error {
	out, err := themedCall("ensureRequestedImage", themedJSON(*version))
	if err != nil {
		return err
	}
	parsed, err := jsParse(out)
	if err != nil {
		return err
	}
	if o := themedObj(parsed); o != nil {
		*version = o
	}
	return nil
}

var _ = fmt.Sprint
