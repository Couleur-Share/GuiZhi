package main

// 主题阅读一族的同步频道（对应 ipc/themed-reading.ipc.ts）：state / get / references / listTasks / preview /
// restorePrevious / remove / exportHtml。生成、取消、续跑等有后台任务的频道见 themed_runtime.go。
// 渲染层（文档拼装、导出文档）与结构校验由 goja 里的 TS 原实现提供，其余逻辑用 Go 重写。

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	chThemedState           = "themedReading:state"
	chThemedPreview         = "themedReading:preview"
	chThemedReferences      = "themedReading:references"
	chThemedGet             = "themedReading:get"
	chThemedRestorePrevious = "themedReading:restorePrevious"
	chThemedRemove          = "themedReading:remove"
	chThemedExport          = "themedReading:exportHtml"
	chThemedListTasks       = "themedReading:listTasks"
	chThemedProgress        = "themedReading:progress"
)

var reThemedInstanceID = regexp.MustCompile(`^[\w-]{1,100}$`)

type themedRequest struct{ itemID, kind string }

func parseThemedRequest(v any) (themedRequest, error) {
	id, ok := jsGet(v, "itemId").(string)
	kind, _ := jsGet(v, "sourceKind").(string)
	if !vTruthy(v) || !ok || jsTrim(id) == "" || utf16Len(id) > 200 || (kind != "body" && kind != "summary") {
		return themedRequest{}, errors.New("主题排版条目或内容来源无效")
	}
	return themedRequest{id, kind}, nil
}

// themedItem 对应 KnowledgeItemDB.get 中主题阅读用到的字段。
type themedItem struct {
	ID, Title, Content, ItemType string
	SourceURI                    *string
	DeletedAt                    *int64
}

func (l *Library) themedItem(ctx context.Context, id string) (*themedItem, error) {
	rows, err := l.db.Query(ctx, `SELECT i.id,i.title,i.content,i.item_type,i.deleted_at,(
         SELECT s.source_uri FROM source_records s WHERE s.item_id = i.id ORDER BY s.captured_at DESC LIMIT 1
       ) AS source_uri FROM knowledge_items i WHERE i.id = ?`, id)
	if err != nil || len(rows.Values) == 0 {
		return nil, err
	}
	r := rows.Values[0]
	str := func(v any) string { s, _ := v.(string); return s }
	it := &themedItem{ID: str(r[0]), Title: str(r[1]), Content: str(r[2]), ItemType: str(r[3])}
	if v, ok := r[4].(int64); ok {
		it.DeletedAt = &v
	}
	if s, ok := r[5].(string); ok {
		it.SourceURI = &s
	}
	return it, nil
}

// themedReadingContent 对应 content.ts 的同名函数：取条目参与排版的正文。
func themedReadingContent(it *themedItem, kind string) (string, error) {
	if kind != "body" && kind != "summary" {
		return "", errors.New("不支持的主题排版内容来源")
	}
	if kind == "summary" {
		if it.ItemType != "forum" {
			return "", errors.New("此条目没有讨论总结")
		}
		return splitForumNoteSections(it.Content).Summary, nil
	}
	switch it.ItemType {
	case "forum":
		return splitForumNoteSections(it.Content).Body, nil
	case "image":
		return splitImageNoteSections(it.Content).Caption, nil
	case "audio", "video":
		if b, ok := videoMetaBody(it.Content); ok {
			return b, nil
		}
	}
	return it.Content, nil
}

// themedFingerprint 标题、正文、来源与来源类型决定过期状态。
func themedFingerprint(title, content string, sourceURI *string, kind string) string {
	var uri any = jsNull
	if sourceURI != nil {
		uri = *sourceURI
	}
	s, _ := jsStringify(jsArr{title, content, uri, kind})
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func themedLatestTask(tasks []*jsObj, req themedRequest) any {
	var best *jsObj
	for _, t := range tasks {
		if themedStr(t, "itemId") != req.itemID || themedStr(t, "sourceKind") != req.kind {
			continue
		}
		if best == nil || jsToNumber(t.get("createdAt")) > jsToNumber(best.get("createdAt")) {
			best = t
		}
	}
	if best == nil {
		return jsUndefined
	}
	return best
}

func themedCopyObj(o *jsObj) *jsObj {
	n := newObj()
	for _, k := range o.keys() {
		n.set(k, o.get(k))
	}
	return n
}

// themedPreviewVersion 对应 v3-pipeline.ts 的 readingPreviewVersion：用已生成的章节拼出静态预览。
func themedPreviewVersion(page *jsObj) *jsObj {
	g, _ := page.get("generation").(*jsObj)
	sections, _ := jsGet(g, "sections").(jsArr)
	if g == nil || len(sections) == 0 {
		return nil
	}
	htmls := make([]string, 0, len(sections))
	for _, s := range sections {
		h, _ := jsGet(s, "html").(string)
		htmls = append(htmls, h)
	}
	direction := ""
	if d, ok := page.get("designDirection").(string); ok {
		direction = d
	}
	design := newObj()
	design.set("html", strings.Join(htmls, "\n"))
	design.set("css", jsGet(g, "css"))
	design.set("direction", direction)
	design.set("assets", jsArr{})
	design.set("scripts", jsArr{})
	design.set("libraries", jsArr{})
	out := themedCopyObj(page)
	out.set("design", design)
	return out
}

// ---- 图片体检（image-health.ts） ----

const themedImageLimit = 20 * 1024 * 1024

var reSHA256 = regexp.MustCompile(`^[a-f0-9]{64}$`)

// themedImageExtension 对应 assets.ts 的 imageExtension：按文件头识别格式。
func themedImageExtension(data []byte) (string, error) {
	if len(data) > themedImageLimit || len(data) < 12 {
		return "", errors.New("图片为空或超过 20 MiB 上限")
	}
	switch {
	case len(data) >= 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n":
		return "png", nil
	case data[0] == 255 && data[1] == 216 && data[2] == 255:
		return "jpg", nil
	case regexp.MustCompile(`^GIF8[79]a`).Match(data[:6]):
		return "gif", nil
	case string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "webp", nil
	}
	return "", errors.New("图片不是受支持的 PNG/JPEG/GIF/WebP 格式")
}

var reThemedImageName = regexp.MustCompile(`(?i)\.(png|jpe?g|gif|webp)$`)

func (l *Library) localThemeAssetPath(name string) (string, error) {
	if !isSafeAssetFileName(name) || !reThemedImageName.MatchString(name) {
		return "", errors.New("主题图片路径不合法")
	}
	return filepath.Join(l.paths.ImagesDir(), name), nil
}

// readThemeAsset 读取并校验一张主题图片，返回字节与 MIME。
func (l *Library) readThemeAsset(asset any) ([]byte, string, error) {
	name, _ := jsGet(asset, "fileName").(string)
	if name == "" {
		return nil, "", fmt.Errorf("图片尚未保存：%s", jsToString(jsGet(asset, "alt")))
	}
	file, err := l.localThemeAssetPath(name)
	if err != nil {
		return nil, "", err
	}
	st, err := os.Stat(file)
	if err != nil {
		return nil, "", nodeFSError(err, "stat", file)
	}
	if st.Size() > themedImageLimit {
		return nil, "", errors.New("图片超过 20 MiB 上限")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, "", nodeFSError(err, "open", file)
	}
	ext, err := themedImageExtension(data)
	if err != nil {
		return nil, "", err
	}
	if sha, _ := jsGet(asset, "sha256").(string); sha != "" {
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != sha {
			return nil, "", fmt.Errorf("图片校验失败：%s", jsToString(jsGet(asset, "alt")))
		}
	}
	mime := "image/" + ext
	if ext == "jpg" {
		mime = "image/jpeg"
	}
	return data, mime, nil
}

type themedHealthCache struct {
	mu sync.Mutex
	m  map[string]string
}

var themedImageVerified = &themedHealthCache{m: map[string]string{}}

func resetThemedImageHealthCache() {
	themedImageVerified.mu.Lock()
	themedImageVerified.m = map[string]string{}
	themedImageVerified.mu.Unlock()
}

func themedVerifyStat(asset any, st os.FileInfo) error {
	if !st.Mode().IsRegular() {
		return errors.New("图片不是可读取的本地文件")
	}
	sha, _ := jsGet(asset, "sha256").(string)
	bytes, isNum := jsGet(asset, "bytes").(float64)
	if !reSHA256.MatchString(sha) || !isNum || bytes != float64(int64(bytes)) {
		return errors.New("图片缺少完整校验信息，请重新生成")
	}
	if float64(st.Size()) != bytes {
		return errors.New("图片文件长度发生变化，请补图或重新生成")
	}
	return nil
}

func (l *Library) themedVerifyAsset(asset any) error {
	name, _ := jsGet(asset, "fileName").(string)
	file, err := l.localThemeAssetPath(name)
	if err != nil {
		return err
	}
	before, err := os.Lstat(file)
	if err != nil {
		return err
	}
	if err := themedVerifyStat(asset, before); err != nil {
		return err
	}
	sig := fmt.Sprintf("%v|%v|%d|%d", jsGet(asset, "sha256"), jsGet(asset, "bytes"), before.Size(), before.ModTime().UnixNano())
	themedImageVerified.mu.Lock()
	cached := themedImageVerified.m[file] == sig
	themedImageVerified.mu.Unlock()
	if cached {
		return nil
	}
	if _, _, err := l.readThemeAsset(asset); err != nil {
		return err
	}
	after, err := os.Lstat(file)
	if err != nil {
		return err
	}
	if err := themedVerifyStat(asset, after); err != nil {
		return err
	}
	if fmt.Sprintf("%v|%v|%d|%d", jsGet(asset, "sha256"), jsGet(asset, "bytes"), after.Size(), after.ModTime().UnixNano()) != sig {
		return errors.New("图片在校验期间发生变化，请重试读取")
	}
	themedImageVerified.mu.Lock()
	if len(themedImageVerified.m) >= 512 {
		themedImageVerified.m = map[string]string{}
	}
	themedImageVerified.m[file] = sig
	themedImageVerified.mu.Unlock()
	return nil
}

func themedHealthError(err error) string {
	if errors.Is(err, os.ErrNotExist) {
		return "图片文件已丢失，请补图或重新生成"
	}
	return err.Error()
}

func themedAssetFiles(v *jsObj) []string {
	var files []string
	list, _ := v.get("assets").(jsArr)
	for _, a := range list {
		if n, _ := jsGet(a, "fileName").(string); n != "" {
			files = append(files, n)
		}
	}
	return files
}

// inspectImages 对应 inspectThemedReadingImages：只降级本次读取的副本，不把磁盘暂时缺失写回已保存的版本。
func (l *Library) themedInspectImages(version *jsObj) (*jsObj, error) {
	view := themedCopyObj(version)
	assets := jsArr{}
	list, _ := version.get("assets").(jsArr)
	for _, a := range list {
		if o, ok := a.(*jsObj); ok {
			assets = append(assets, themedCopyObj(o))
		} else {
			assets = append(assets, a)
		}
	}
	view.set("assets", assets)
	warnings := jsArr{}
	if w, ok := version.get("warnings").(jsArr); ok {
		warnings = append(warnings, w...)
	}
	view.set("warnings", warnings)
	release, err := retainAssetFiles(themedAssetFiles(view))
	if err != nil {
		return nil, err
	}
	defer release()
	for _, a := range assets {
		asset, ok := a.(*jsObj)
		if !ok || themedStr(asset, "status") != "ready" {
			continue
		}
		if err := l.themedVerifyAsset(asset); err != nil {
			msg := themedHealthError(err)
			asset.set("status", "failed")
			asset.set("error", msg)
			label, _ := asset.get("alt").(string)
			if label == "" {
				label, _ = asset.get("purpose").(string)
			}
			if label == "" {
				label = "页面图片"
			}
			warning := label + "：" + msg
			dup := false
			for _, w := range warnings {
				if w == warning {
					dup = true
				}
			}
			if !dup {
				warnings = append(warnings, warning)
				view.set("warnings", warnings)
			}
		}
	}
	return view, nil
}

func (l *Library) themedAssertImagesHealthy(version *jsObj, allowKnownIncomplete bool) error {
	view, err := l.themedInspectImages(version)
	if err != nil {
		return err
	}
	previouslyReady := map[string]bool{}
	list, _ := version.get("assets").(jsArr)
	for _, a := range list {
		if themedStr(a, "status") == "ready" {
			previouslyReady[themedStr(a, "id")] = true
		}
	}
	var failures []string
	viewAssets, _ := view.get("assets").(jsArr)
	for _, a := range viewAssets {
		if themedStr(a, "status") == "ready" || (allowKnownIncomplete && !previouslyReady[themedStr(a, "id")]) {
			continue
		}
		label, _ := jsGet(a, "alt").(string)
		if label == "" {
			label, _ = jsGet(a, "purpose").(string)
		}
		reason, _ := jsGet(a, "error").(string)
		if reason == "" {
			reason = "图片尚未完成"
		}
		failures = append(failures, label+"："+reason)
	}
	if len(failures) > 0 {
		return errors.New("上一版图片不完整，当前页面保持不变：" + strings.Join(failures, "；"))
	}
	return nil
}

// ---- 导出（export.ts） ----

const themedExportLimit = 100 * 1024 * 1024

func (l *Library) themedExportHTML(version *jsObj, withoutImages, staticOnly bool) (string, error) {
	release, err := retainAssetFiles(themedAssetFiles(version))
	if err != nil {
		return "", err
	}
	defer release()
	urls := newObj()
	bytes := 0
	if !withoutImages {
		list, _ := version.get("assets").(jsArr)
		for _, a := range list {
			if themedStr(a, "status") != "ready" {
				return "", fmt.Errorf("素材不完整：%s。请补生成，或选择无图导出。", jsToString(jsGet(a, "alt")))
			}
			data, mime, err := l.readThemeAsset(a)
			if err != nil {
				return "", err
			}
			bytes += (len(data) + 2) / 3 * 4
			if bytes > themedExportLimit {
				return "", errors.New("单文件 HTML 超过 100 MiB，请使用无图导出")
			}
			urls.set(themedStr(a, "id"), "data:"+mime+";base64,"+base64.StdEncoding.EncodeToString(data))
		}
	}
	html, err := themedCall("exportDocument", themedJSON(version), themedJSON(urls), staticOnly)
	if err != nil {
		return "", err
	}
	if len(html) > themedExportLimit {
		return "", errors.New("单文件 HTML 超过 100 MiB，请使用无图导出")
	}
	has, err := themedCall("hasLocalImageRef", html)
	if err != nil {
		return "", err
	}
	if has == "true" {
		return "", errors.New("导出仍含本地图片引用，未写入文件")
	}
	return html, nil
}

// ---- 频道 ----

func themedFail(l *Library, action string, err error) any {
	msg := err.Error()
	l.paths.logAppError("themedReading", action, msg, nil)
	return map[string]any{"success": false, "error": msg}
}

// themedUUIDHook 仅供测试：让两端按「第 n 次生成」的计数器对拍生成次序。
var themedUUIDHook func() string

func themedUUID() string {
	if themedUUIDHook != nil {
		return themedUUIDHook()
	}
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// themedModelNames 对应 get 通道的 models 字段。
func (l *Library) themedModelNames() map[string]any {
	out := map[string]any{"text": nil, "image": nil}
	if c := l.resolveMediaSummaryConfig(); c != nil {
		out["text"] = c.Model
	}
	if c := l.resolveImageGenConfig(); c != nil {
		out["image"] = c.Model
	}
	return out
}

// resolveImageGenConfig 对应 image-gen.ts：imageGen 路由指定的模型，凭据不全视为未配置。
func (l *Library) resolveImageGenConfig() *aiClientConfig {
	cfg, err := l.readAIConfig()
	if err != nil {
		return nil
	}
	id, _ := cfg.Routes.get("imageGen")
	want, _ := id.(string)
	if want == "" {
		return nil
	}
	for _, m := range cfg.Models {
		if m.vals["id"] != want {
			continue
		}
		str := func(k string) string { s, _ := m.vals[k].(string); return jsTrim(s) }
		out := &aiClientConfig{APIURL: str("apiUrl"), APIKey: str("apiKey"), Model: str("model")}
		out.Provider, _ = m.vals["provider"].(string)
		out.APIProtocol, _ = m.vals["apiProtocol"].(string)
		if out.APIURL == "" || out.APIKey == "" || out.Model == "" {
			return nil
		}
		return out
	}
	return nil
}

func (l *Library) registerThemedReading(b *Bridge) {
	pages := l.themedPages()
	pages.startup(context.Background())
	wrap := func(action string, fn func(ctx context.Context, a []json.RawMessage) (any, error)) func(context.Context, []json.RawMessage) (any, error) {
		return func(ctx context.Context, a []json.RawMessage) (any, error) {
			out, err := fn(ctx, a)
			if err != nil {
				return themedFail(l, action, err), nil
			}
			return out, nil
		}
	}
	requireItem := func(ctx context.Context, id string) (*themedItem, error) {
		it, err := l.themedItem(ctx, id)
		if err != nil {
			return nil, err
		}
		if it == nil {
			return nil, errors.New("条目不存在")
		}
		return it, nil
	}
	b.handlers[chThemedPreview] = wrap("preview", func(ctx context.Context, a []json.RawMessage) (any, error) {
		raw := jsArg(a, 0)
		req, err := parseThemedRequest(raw)
		if err != nil {
			return nil, err
		}
		task, err := pages.getTask(ctx, jsToString(jsGet(raw, "taskId")))
		if err != nil {
			return nil, err
		}
		if task == nil || themedStr(task, "itemId") != req.itemID || themedStr(task, "sourceKind") != req.kind {
			return nil, errors.New("生成任务不属于当前文章")
		}
		version, err := pages.getVersion(ctx, themedStr(task, "versionId"))
		if err != nil {
			return nil, err
		}
		out := map[string]any{"success": true}
		if version != nil {
			if page := themedPreviewVersion(version); page != nil {
				revision := jsGet(page.get("generation"), "revision")
				out["preview"] = map[string]any{"page": page, "revision": revision}
			}
		}
		return out, nil
	})
	b.handlers[chThemedReferences] = wrap("references", func(ctx context.Context, a []json.RawMessage) (any, error) {
		req, err := parseThemedRequest(jsArg(a, 0))
		if err != nil {
			return nil, err
		}
		page, err := pages.get(ctx, req.itemID, req.kind, "")
		if err != nil {
			return nil, err
		}
		refs := []any{}
		if page != nil {
			if rec, ok := page.get("reconstruction").(*jsObj); ok {
				used := map[any]bool{}
				draft, _ := rec.get("draft").(jsArr)
				for _, s := range draft {
					ids, _ := jsGet(s, "referenceIds").(jsArr)
					for _, id := range ids {
						used[id] = true
					}
				}
				list, _ := rec.get("references").(jsArr)
				for _, r := range list {
					o, ok := r.(*jsObj)
					if !ok || themedStr(o, "status") != "ready" || !used[o.get("id")] {
						continue
					}
					c := newObj()
					for _, k := range o.keys() {
						if k != "text" {
							c.set(k, o.get(k))
						}
					}
					refs = append(refs, c)
				}
			}
		}
		return map[string]any{"success": true, "references": refs}, nil
	})
	b.handlers[chThemedState] = wrap("state", func(ctx context.Context, a []json.RawMessage) (any, error) {
		req, err := parseThemedRequest(jsArg(a, 0))
		if err != nil {
			return nil, err
		}
		item, err := requireItem(ctx, req.itemID)
		if err != nil {
			return nil, err
		}
		page, err := pages.get(ctx, req.itemID, req.kind, "")
		if err != nil {
			return nil, err
		}
		tasks, err := pages.listTasks(ctx)
		if err != nil {
			return nil, err
		}
		state := newObj()
		state.set("hasPage", page != nil)
		stale := false
		if page != nil {
			state.set("versionId", page.get("id"))
			state.set("formatVersion", page.get("formatVersion"))
		}
		if t := themedLatestTask(tasks, req); t != jsUndefined {
			state.set("task", t)
		}
		if page != nil {
			content, err := themedReadingContent(item, req.kind)
			if err != nil {
				return nil, err
			}
			stale = themedStr(jsGet(page, "source"), "fingerprint") != themedFingerprint(item.Title, content, item.SourceURI, req.kind)
		}
		state.set("stale", stale)
		return map[string]any{"success": true, "state": state}, nil
	})
	b.handlers[chThemedGet] = wrap("get", func(ctx context.Context, a []json.RawMessage) (any, error) {
		raw := jsArg(a, 0)
		req, err := parseThemedRequest(raw)
		if err != nil {
			return nil, err
		}
		item, err := requireItem(ctx, req.itemID)
		if err != nil {
			return nil, err
		}
		stored, err := pages.get(ctx, req.itemID, req.kind, "")
		if err != nil {
			return nil, err
		}
		metadataOnly := jsGet(raw, "metadataOnly") == true
		page := stored
		if stored != nil && !metadataOnly {
			if page, err = l.themedInspectImages(stored); err != nil {
				return nil, err
			}
		}
		instanceID := ""
		if s, ok := jsGet(raw, "instanceId").(string); ok && reThemedInstanceID.MatchString(s) {
			instanceID = s
		}
		content, err := themedReadingContent(item, req.kind)
		if err != nil {
			return nil, err
		}
		tasks, err := pages.listTasks(ctx)
		if err != nil {
			return nil, err
		}
		out := map[string]any{"success": true}
		if st, err := l.readingSearchStatus(); err != nil {
			l.paths.logAppError("themedReading", "searchStatus", err.Error(), nil)
		} else {
			out["search"] = st
		}
		if page != nil {
			out["page"] = page
		} else {
			out["page"] = nil
		}
		prev, err := pages.get(ctx, req.itemID, req.kind, "previous")
		if err != nil {
			return nil, err
		}
		out["previous"] = prev != nil
		if t := themedLatestTask(tasks, req); t != jsUndefined {
			out["task"] = t
		}
		out["stale"] = page != nil && themedStr(jsGet(page, "source"), "fingerprint") != themedFingerprint(item.Title, content, item.SourceURI, req.kind)
		if page != nil && jsToNumber(page.get("formatVersion")) != 3 && !metadataOnly {
			var inst any
			if instanceID != "" {
				inst = instanceID
			}
			doc, err := themedCall("document", themedJSON(page), inst, nil)
			if err != nil {
				return nil, err
			}
			out["document"] = doc
		}
		out["models"] = l.themedModelNames()
		return out, nil
	})
	b.handlers[chThemedListTasks] = wrap("listTasks", func(ctx context.Context, _ []json.RawMessage) (any, error) {
		tasks, err := pages.listTasks(ctx)
		if err != nil {
			return nil, err
		}
		list := jsArr{}
		for _, t := range tasks {
			list = append(list, t)
		}
		return map[string]any{"success": true, "tasks": list}, nil
	})
	b.handlers[chThemedRestorePrevious] = wrap("restorePrevious", func(ctx context.Context, a []json.RawMessage) (any, error) {
		req, err := parseThemedRequest(jsArg(a, 0))
		if err != nil {
			return nil, err
		}
		page, err := pages.get(ctx, req.itemID, req.kind, "previous")
		if err != nil {
			return nil, err
		}
		if page == nil {
			return nil, errors.New("没有上一可用版本")
		}
		if _, err := themedCall("document", themedJSON(page), nil, nil); err != nil {
			return nil, err
		}
		if err := l.themedAssertImagesHealthy(page, true); err != nil {
			return nil, err
		}
		again, err := pages.get(ctx, req.itemID, req.kind, "previous")
		if err != nil {
			return nil, err
		}
		if again == nil || themedStr(again, "id") != themedStr(page, "id") {
			return nil, errors.New("上一版已变化，请刷新后重试")
		}
		restored, err := pages.restorePrevious(ctx, req.itemID, req.kind)
		if err != nil {
			return nil, err
		}
		var out any = jsNull
		if restored != nil {
			out = restored
		}
		return map[string]any{"success": true, "page": out}, nil
	})
	b.handlers[chThemedRemove] = wrap("remove", func(ctx context.Context, a []json.RawMessage) (any, error) {
		req, err := parseThemedRequest(jsArg(a, 0))
		if err != nil {
			return nil, err
		}
		item, err := l.themedItem(ctx, req.itemID)
		if err != nil {
			return nil, err
		}
		if item == nil || item.DeletedAt != nil {
			return nil, errors.New("回收站中的主题页只读，请先恢复条目")
		}
		tasks, err := pages.listTasks(ctx)
		if err != nil {
			return nil, err
		}
		for _, t := range tasks {
			if themedStr(t, "itemId") == req.itemID && themedStr(t, "sourceKind") == req.kind && themedActive(themedStr(t, "state")) {
				if _, err := l.themedCancel(ctx, themedStr(t, "id")); err != nil {
					return nil, err
				}
			}
		}
		files, err := pages.remove(ctx, req.itemID, req.kind)
		if err != nil {
			return nil, err
		}
		if _, err := l.cleanupOrphanAssets(ctx, files); err != nil {
			return nil, err
		}
		return map[string]any{"success": true}, nil
	})
	b.handlers[chThemedExport] = wrap("export", func(ctx context.Context, a []json.RawMessage) (any, error) {
		raw := jsArg(a, 0)
		req, err := parseThemedRequest(raw)
		if err != nil {
			return nil, err
		}
		var page *jsObj
		if vTruthy(jsGet(raw, "versionId")) {
			page, err = pages.getVersion(ctx, jsToString(jsGet(raw, "versionId")))
		} else {
			page, err = pages.get(ctx, req.itemID, req.kind, "")
		}
		if err != nil {
			return nil, err
		}
		if page == nil {
			return nil, errors.New("还没有可导出的主题页")
		}
		if themedStr(page, "itemId") != req.itemID || themedStr(page, "sourceKind") != req.kind || themedStr(page, "role") == "working" {
			return nil, errors.New("导出版本不属于当前文章或尚未发布")
		}
		html, err := l.themedExportHTML(page, jsGet(raw, "withoutImages") == true, jsGet(raw, "staticOnly") == true)
		if err != nil {
			return nil, err
		}
		title := themedStr(jsGet(page, "source"), "title")
		name := reThemedBadFileChars.ReplaceAllString(title, "_")
		if r := []rune(name); len(r) > 100 {
			name = string(r[:100])
		}
		if name == "" {
			name = "主题阅读页"
		}
		if l.saveFileDialog == nil {
			return nil, errors.New("保存对话框不可用")
		}
		dest, err := l.saveFileDialog("导出主题阅读页", name+".html", []dialogFilter{{Name: "HTML", Exts: []string{"html"}}})
		if err != nil {
			return nil, err
		}
		if dest == "" {
			return map[string]any{"success": true, "cancelled": true}, nil
		}
		staged := dest + "." + themedUUID() + ".tmp"
		defer os.Remove(staged)
		f, err := os.OpenFile(staged, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return nil, err
		}
		if _, err := f.WriteString(html); err != nil {
			f.Close()
			return nil, err
		}
		if err := f.Close(); err != nil {
			return nil, err
		}
		if err := os.Rename(staged, dest); err != nil {
			return nil, err
		}
		return map[string]any{"success": true, "path": dest}, nil
	})
	l.registerThemedRuntime(b)
}

var reThemedBadFileChars = regexp.MustCompile("[<>:\"/\\\\|?*\\x00-\\x1f]")

// themedNotify 向渲染进程推送任务进度（对应 runtime.notify → themedReading:progress）。
func (l *Library) themedNotify(task *jsObj) {
	l.pushMu.RLock()
	push := l.push
	l.pushMu.RUnlock()
	if push != nil {
		push(chThemedProgress, task)
	}
}

// themedCancel 对应 ThemedReadingRuntime.cancel：把活动任务标为已取消；执行中的任务由运行时中止。
func (l *Library) themedCancel(ctx context.Context, id string) (*jsObj, error) {
	pages := l.themedPages()
	task, err := pages.getTask(ctx, id)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, errors.New("任务不存在")
	}
	if !themedActive(themedStr(task, "state")) {
		return task, nil
	}
	task.set("state", "cancelled")
	task.set("error", "已停止；已发出的模型请求可能已计费，可手动继续未完成步骤")
	task.set("updatedAt", float64(time.Now().UnixMilli()))
	saved, err := pages.saveTask(ctx, task)
	if err != nil {
		return nil, err
	}
	l.themedNotify(saved)
	if l.themedAbort != nil {
		l.themedAbort(id)
	}
	return saved, nil
}

// nodeFSError 把「文件不存在」改写成 Node 的 ENOENT 文案（调用方据此给出统一的中文提示），其余原样返回。
type nodeENOENT struct{ msg string }

func (e *nodeENOENT) Error() string        { return e.msg }
func (e *nodeENOENT) Is(target error) bool { return target == os.ErrNotExist }

func nodeFSError(err error, op, path string) error {
	if errors.Is(err, os.ErrNotExist) {
		return &nodeENOENT{fmt.Sprintf("ENOENT: no such file or directory, %s '%s'", op, path)}
	}
	return err
}
