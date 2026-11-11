package main

// 对应 ipc/web-capture.ipc.ts 中的 web:snapshot / web:snapshot-export / web:snapshot-enqueue / web:solverCheck / web:versions，
// 以及 services/web-capture/snapshot-service.ts：读取公众号原文快照、导出为 HTML 目录、批量补采、FlareSolverr 连通性检查。
// 快照净化与文档拼装在 goja 里运行 TS 原文件的打包产物（见 websnap_engine.go），与 Electron 版逐字节一致。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	whatwg "github.com/nlnwa/whatwg-url/url"
)

// del 对应 delete obj[k]。
func (o *jsObj) del(k string) {
	if _, ok := o.m[k]; !ok {
		return
	}
	delete(o.m, k)
	for i, name := range o.order {
		if name == k {
			o.order = append(o.order[:i], o.order[i+1:]...)
			break
		}
	}
}

// webRecordID 对应 web-capture.ipc.ts 的 id()：非空字符串且 UTF-16 长度不超过 100。
func webRecordID(v any) (string, error) {
	s, ok := v.(string)
	if !ok || s == "" || utf16Len(s) > 100 {
		return "", errors.New("记录 ID 无效")
	}
	return s, nil
}

// webOK 对应 response()：成功包成 {ok,data}，失败只带原因。
func webResponse(data any, err error) map[string]any {
	if err != nil {
		msg := err.Error()
		if msg == "" {
			msg = "网页操作失败"
		}
		return map[string]any{"ok": false, "error": msg}
	}
	return map[string]any{"ok": true, "data": jsToPlain(data)}
}

// isWechatURL 对应 wechat.ts 的 isWechatUrl：http(s) 且主机名恰为 mp.weixin.qq.com。
func isWechatURL(raw string) bool {
	u, err := whatwg.Parse(raw)
	if err != nil {
		return false
	}
	if p := u.Protocol(); p != "https:" && p != "http:" {
		return false
	}
	return u.Hostname() == "mp.weixin.qq.com"
}

type webItemRecord struct {
	Title, Content string
	Deleted        bool
}

// webItemAny 对应 KnowledgeItemDB.get：已删除的条目也会返回（调用方自行判断 deletedAt）。
func (l *Library) webItemAny(ctx context.Context, itemID string) (*webItemRecord, error) {
	rows, err := l.db.Query(ctx, "SELECT title,content,deleted_at FROM knowledge_items WHERE id=?", itemID)
	if err != nil || len(rows.Values) == 0 {
		return nil, err
	}
	r := rows.Values[0]
	str := func(v any) string { s, _ := v.(string); return s }
	return &webItemRecord{Title: str(r[0]), Content: str(r[1]), Deleted: r[2] != nil}, nil
}

// webVersionObjs 对应 WebSourceDB.versions：保留 payload 的键序与未知字段。
func (l *Library) webVersionObjs(ctx context.Context, itemID string) ([]*jsObj, error) {
	rows, err := l.db.Query(ctx, "SELECT payload FROM web_source_versions WHERE item_id=? ORDER BY captured_at DESC,rowid DESC", itemID)
	if err != nil {
		return nil, err
	}
	out := make([]*jsObj, 0, len(rows.Values))
	for _, r := range rows.Values {
		raw, _ := r[0].(string)
		v, err := jsParse(raw)
		if err != nil {
			return nil, fmt.Errorf("原文版本记录损坏：%w", err)
		}
		o, ok := v.(*jsObj)
		if !ok {
			return nil, errors.New("原文版本记录损坏")
		}
		out = append(out, o)
	}
	return out, nil
}

// webSnapshotSource 对应 snapshotSource：公众号条目的原文地址。
func (l *Library) webSnapshotSource(ctx context.Context, itemID string) (string, error) {
	item, err := l.webItemAny(ctx, itemID)
	if err != nil {
		return "", err
	}
	if item == nil || item.Deleted {
		return "", errors.New("条目不存在或已删除")
	}
	rows, err := l.db.Query(ctx, "SELECT source_uri,access_uri FROM source_records WHERE item_id=? ORDER BY captured_at DESC LIMIT 1", itemID)
	if err != nil {
		return "", err
	}
	url := ""
	if len(rows.Values) > 0 {
		access, _ := rows.Values[0][1].(string)
		source, _ := rows.Values[0][0].(string)
		url = access
		if url == "" {
			url = source
		}
	}
	if !isWechatURL(url) {
		return "", errors.New("此条目不是微信公众号文章")
	}
	return url, nil
}

// numberOf 把 jsvalue 数字取出来；非数字返回 false（JS 里与之比较恒为 false）。
func numberOf(v any) (float64, bool) {
	f, ok := v.(float64)
	return f, ok
}

// webReadSnapshot 对应 readSnapshot：返回 WebSnapshotView 形状的 jsvalue 对象。
func (l *Library) webReadSnapshot(ctx context.Context, itemID, versionID string, hasVersionID bool) (*jsObj, error) {
	item, err := l.webItemAny(ctx, itemID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, errors.New("条目不存在")
	}
	baseline, err := l.webSourceBaseline(ctx, itemID)
	if err != nil {
		return nil, err
	}
	versions, err := l.webVersionObjs(ctx, itemID)
	if err != nil {
		return nil, err
	}
	baselineVersion := ""
	if baseline != nil {
		baselineVersion = baseline.VersionID
	}
	hasSnapshot := func(v *jsObj) bool { return vTruthy(v.get("snapshot")) }
	var version *jsObj
	if hasVersionID {
		for _, v := range versions {
			if id, _ := v.get("id").(string); id == versionID {
				version = v
				break
			}
		}
	} else {
		for _, v := range versions {
			if id, _ := v.get("id").(string); baseline != nil && id == baselineVersion && hasSnapshot(v) {
				version = v
				break
			}
		}
		if version == nil {
			for _, v := range versions {
				if hasSnapshot(v) {
					version = v
					break
				}
			}
		}
	}
	if hasVersionID && version == nil {
		return nil, errors.New("原文版本不存在")
	}
	edited := baseline != nil && webContentHash(item.Content) != baseline.ContentHash
	view := func(extra func(*jsObj)) *jsObj {
		o := newObj()
		o.set("version", jsNull)
		o.set("edited", edited)
		o.set("pending", false)
		if extra != nil {
			extra(o)
		}
		return o
	}
	if version == nil || !hasSnapshot(version) {
		return view(nil), nil
	}
	fail := func(err error) (*jsObj, error) {
		msg := err.Error()
		if msg == "" {
			msg = "原文快照损坏"
		}
		return view(func(o *jsObj) { o.set("error", msg) }), nil
	}
	snapJSON, _ := jsStringify(version.get("snapshot"))
	cleaned, err := snapshotSanitize(snapJSON)
	if err != nil {
		return fail(err)
	}
	snapshot, err := jsParse(cleaned)
	if err != nil {
		return fail(err)
	}
	version.set("snapshot", snapshot)
	if assets, ok := jsGet(snapshot, "assets").(jsArr); ok {
		for _, a := range assets {
			name, _ := jsGet(a, "fileName").(string)
			want, _ := jsGet(a, "sha256").(string)
			data, err := os.ReadFile(filepath.Join(l.paths.ImagesDir(), name))
			if err != nil {
				if os.IsNotExist(err) {
					return fail(fmt.Errorf("原文图片缺失（%s），请重新补采", name))
				}
				return fail(err)
			}
			sum := sha256.Sum256(data)
			if hex.EncodeToString(sum[:]) != want {
				return fail(errors.New("原文图片校验失败，请重新补采"))
			}
		}
	}
	instanceID := newUUID()
	capturedAt, hasCaptured := numberOf(version.get("capturedAt"))
	pending := false
	for _, v := range versions {
		if !hasSnapshot(v) {
			continue
		}
		if id, _ := v.get("id").(string); baseline != nil && id == baselineVersion {
			continue
		}
		if at, ok := numberOf(v.get("capturedAt")); ok && hasCaptured && at > capturedAt {
			pending = true
			break
		}
	}
	document, err := snapshotDocumentOf(cleaned, instanceID)
	if err != nil {
		return fail(err)
	}
	out := newObj()
	out.set("version", version)
	out.set("edited", edited)
	out.set("pending", pending)
	out.set("instanceId", instanceID)
	out.set("document", document)
	return out, nil
}

// renameBusy 对应 Windows 扫描器短暂占用刚写完目录时的 EPERM/EBUSY/EACCES。
func renameBusy(err error) bool {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case 5, 32, 33: // ERROR_ACCESS_DENIED / SHARING_VIOLATION / LOCK_VIOLATION
			return true
		}
	}
	return os.IsPermission(err)
}

// webExportSnapshot 对应 exportSnapshot：把某版本的原文排版导成 index.html + assets 目录。
func (l *Library) webExportSnapshot(ctx context.Context, itemID, versionID string) (any, error) {
	view, err := l.webReadSnapshot(ctx, itemID, versionID, true)
	if err != nil {
		return nil, err
	}
	version, _ := view.get("version").(*jsObj)
	var snapshot any = jsUndefined
	if version != nil {
		snapshot = version.get("snapshot")
	}
	if !vTruthy(snapshot) {
		msg, _ := view.get("error").(string)
		if msg == "" {
			msg = "此版本没有原文排版"
		}
		return nil, errors.New(msg)
	}
	if l.pickDirectory == nil {
		return map[string]any{"canceled": true}, nil
	}
	parent, err := l.pickDirectory("选择 HTML 导出目录")
	if err != nil {
		return nil, err
	}
	if parent == "" {
		return map[string]any{"canceled": true}, nil
	}
	name := fmt.Sprintf("公众号原文-%d-%s", time.Now().UnixMilli(), newUUID()[:6])
	stage := filepath.Join(parent, "."+name+".tmp")
	target := filepath.Join(parent, name)
	if err := os.MkdirAll(filepath.Join(stage, "assets"), 0o755); err != nil {
		return nil, err
	}
	cleanup := func(err error) (any, error) {
		_ = os.RemoveAll(stage)
		return nil, err
	}
	assets, _ := jsGet(snapshot, "assets").(jsArr)
	for _, a := range assets {
		fileName, _ := jsGet(a, "fileName").(string)
		data, err := os.ReadFile(filepath.Join(l.paths.ImagesDir(), fileName))
		if err != nil {
			return cleanup(err)
		}
		if err := os.WriteFile(filepath.Join(stage, "assets", fileName), data, 0o644); err != nil {
			return cleanup(err)
		}
	}
	snapJSON, _ := jsStringify(snapshot)
	document, err := snapshotDocumentOf(snapJSON, "")
	if err != nil {
		return cleanup(err)
	}
	for _, a := range assets {
		fileName, _ := jsGet(a, "fileName").(string)
		document = strings.ReplaceAll(document, "local-image://"+fileName, "assets/"+fileName)
	}
	sourceURL, _ := version.get("sourceUrl").(string)
	if !isWechatURL(sourceURL) {
		sourceURL = "https://mp.weixin.qq.com/"
	}
	esc := func(v any) (string, error) { return snapshotEscapeText(jsToStringNoUndef(v)) }
	title, err1 := esc(version.get("title"))
	account, err2 := esc(jsGet(snapshot, "account"))
	author, err3 := esc(jsGet(snapshot, "author"))
	href, err4 := esc(sourceURL)
	if err := errors.Join(err1, err2, err3, err4); err != nil {
		return cleanup(err)
	}
	capturedAt, _ := numberOf(version.get("capturedAt"))
	metadata := fmt.Sprintf(`<header><h1>%s</h1><p>%s %s</p><p>采集于 %s</p><p><a href="%s" rel="noreferrer">打开原文</a></p></header>`,
		title, account, author, time.UnixMilli(int64(capturedAt)).UTC().Format("2006-01-02T15:04:05.000Z"), href)
	document = strings.Replace(document, "<body>", "<body>"+metadata, 1)
	if err := os.WriteFile(filepath.Join(stage, "index.html"), []byte(document), 0o644); err != nil {
		return cleanup(err)
	}
	// Windows 扫描器可能短暂占用刚写完的目录；有限重试，不退回非原子复制。
	for attempt := 0; ; attempt++ {
		err := os.Rename(stage, target)
		if err == nil {
			break
		}
		if runtime.GOOS != "windows" || !renameBusy(err) || attempt >= 4 {
			return cleanup(err)
		}
		time.Sleep(time.Duration(100<<attempt) * time.Millisecond)
	}
	failures, _ := jsGet(snapshot, "failures").(jsArr)
	warnings, _ := jsGet(snapshot, "warnings").(jsArr)
	return map[string]any{"path": target, "incomplete": len(failures) > 0 || len(warnings) > 0}, nil
}

// jsToStringNoUndef 对应模板字符串里的 ${x}：undefined 会写成 "undefined"，与 JS 一致。
func jsToStringNoUndef(v any) string { return jsToString(v) }

// webVersionsPayload 对应 web:versions 的返回：版本列表（快照再次净化）、正文与待采用状态。
func (l *Library) webVersionsPayload(ctx context.Context, itemID string) (any, error) {
	item, err := l.webItemAny(ctx, itemID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, errors.New("条目不存在")
	}
	versions, err := l.webVersionObjs(ctx, itemID)
	if err != nil {
		return nil, err
	}
	for _, v := range versions {
		if !vTruthy(v.get("snapshot")) {
			continue
		}
		snapJSON, _ := jsStringify(v.get("snapshot"))
		cleaned, err := snapshotSanitize(snapJSON)
		if err == nil {
			var parsed any
			if parsed, err = jsParse(cleaned); err == nil {
				v.set("snapshot", parsed)
			}
		}
		if err != nil {
			v.del("snapshot") // 对应 try/catch 里 delete v.snapshot
		}
	}
	baseline, err := l.webSourceBaseline(ctx, itemID)
	if err != nil {
		return nil, err
	}
	var latest *jsObj
	for _, v := range versions {
		if v.get("kind") == "remote" && vTruthy(v.get("complete")) {
			latest = v
			break
		}
	}
	list := make(jsArr, len(versions))
	for i, v := range versions {
		list[i] = v
	}
	pending := false
	if latest != nil {
		id, _ := latest.get("id").(string)
		pending = baseline == nil || id != baseline.VersionID
	}
	out := newObj()
	out.set("versions", list)
	out.set("content", item.Content)
	out.set("title", item.Title)
	out.set("contentHash", webContentHash(item.Content))
	out.set("summaryStale", baseline != nil && baseline.SummaryStale != 0)
	out.set("pendingVersion", pending)
	return out, nil
}

func (l *Library) registerWebSnapshot(b *Bridge) {
	b.handlers["web:snapshot"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		itemID, err := webRecordID(jsArg(a, 0))
		if err != nil {
			return webResponse(nil, err), nil
		}
		var versionID string
		// 桥接层的 JSON 参数里 undefined 与 null 无法区分（渲染进程传 undefined 会变成 null），统一当作“未指定”。
		versionArg := jsArg(a, 1)
		hasVersion := !isNullish(versionArg)
		if hasVersion {
			if versionID, err = webRecordID(versionArg); err != nil {
				return webResponse(nil, err), nil
			}
		}
		view, err := l.webReadSnapshot(ctx, itemID, versionID, hasVersion)
		return webResponse(view, err), nil
	}
	b.handlers["web:snapshot-export"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		itemID, err := webRecordID(jsArg(a, 0))
		if err != nil {
			return webResponse(nil, err), nil
		}
		versionID, err := webRecordID(jsArg(a, 1))
		if err != nil {
			return webResponse(nil, err), nil
		}
		out, err := l.webExportSnapshot(ctx, itemID, versionID)
		return webResponse(out, err), nil
	}
	b.handlers["web:snapshot-enqueue"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		ids, ok := jsArg(a, 0).(jsArr)
		if !ok || len(ids) == 0 || len(ids) > 50 {
			return webResponse(nil, errors.New("单次补采 1–50 篇文章")), nil
		}
		var unique []string
		seen := map[string]bool{}
		for _, raw := range ids {
			id, err := webRecordID(raw)
			if err != nil {
				return webResponse(nil, err), nil
			}
			if !seen[id] {
				seen[id] = true
				unique = append(unique, id)
			}
		}
		inputs := make(jsArr, 0, len(unique))
		for _, id := range unique {
			url, err := l.webSnapshotSource(ctx, id)
			if err != nil {
				return webResponse(nil, err), nil
			}
			in := newObj()
			in.set("kind", "url")
			in.set("input", url)
			in.set("refreshOfItemId", id)
			in.set("forceDuplicate", true)
			inputs = append(inputs, in)
		}
		if l.imports == nil {
			return webResponse(nil, errors.New("导入服务尚未就绪")), nil
		}
		tasks, err := l.imports.Enqueue(ctx, inputs)
		return webResponse(tasks, err), nil
	}
	b.handlers["web:solverCheck"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		settings, err := validateFlareSolverr(jsToPlain(jsArg(a, 0)))
		if err != nil {
			return webResponse(nil, err), nil
		}
		out, err := checkFlareSolverrConnection(ctx, settings)
		return webResponse(out, err), nil
	}
	b.handlers["web:versions"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		itemID, err := webRecordID(jsArg(a, 0))
		if err != nil {
			return webResponse(nil, err), nil
		}
		out, err := l.webVersionsPayload(ctx, itemID)
		return webResponse(out, err), nil
	}
}
