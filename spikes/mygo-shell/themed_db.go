package main

// 主题阅读页的数据层，对应 packages/db/src/themed-reading.ts 的 ThemedReadingDB。
// 版本 / 任务的 payload 是 TS 写入的 JSON（保留键顺序），结构校验复用打进 goja 的 TS 原校验器，
// 文案与 Electron 版一致。MyGo 的事务只能提交一批语句、不能在事务内穿插读取，
// 因此「先读后写」的 TS 事务改为：持 writeMu 完成全部读取与判断，再一次性提交写语句。

import (
	"context"
	"errors"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
)

type themedPages struct {
	l   *Library
	now func() int64 // 测试替换；默认毫秒时间戳
}

func (l *Library) themedPages() *themedPages {
	return &themedPages{l: l, now: func() int64 { return time.Now().UnixMilli() }}
}

// 校验器经 goja 调用 TS 原实现；抛出的 Error.message 即 TS 文案。
func themedValidate(kind, payload string) error {
	_, err := themedCall(kind, payload)
	return err
}

func themedStr(o any, key string) string { s, _ := jsGet(o, key).(string); return s }

func (p *themedPages) versionFromRow(r []any) (*jsObj, error) {
	if r == nil {
		return nil, nil
	}
	payload, _ := r[4].(string)
	if err := themedValidate("validateVersion", payload); err != nil {
		return nil, err
	}
	v, err := jsParse(payload)
	if err != nil {
		return nil, err
	}
	o := v.(*jsObj)
	role, _ := r[3].(string)
	if themedStr(o, "id") != r[0] || themedStr(o, "itemId") != r[1] || themedStr(o, "sourceKind") != r[2] || themedStr(o, "role") != role {
		return nil, errors.New("主题阅读页记录与索引不一致")
	}
	return o, nil
}

func (p *themedPages) taskFromRow(r []any) (*jsObj, error) {
	if r == nil {
		return nil, nil
	}
	payload, _ := r[5].(string)
	if err := themedValidate("validateTask", payload); err != nil {
		return nil, err
	}
	v, err := jsParse(payload)
	if err != nil {
		return nil, err
	}
	o := v.(*jsObj)
	if themedStr(o, "id") != r[0] || themedStr(o, "itemId") != r[1] || themedStr(o, "sourceKind") != r[2] ||
		themedStr(o, "versionId") != r[3] || themedStr(o, "state") != r[4] {
		return nil, errors.New("主题阅读页任务与索引不一致")
	}
	return o, nil
}

const themedVersionCols = "id,item_id,source_kind,role,payload"
const themedTaskCols = "id,item_id,source_kind,version_id,state,payload"

func (p *themedPages) oneRow(ctx context.Context, sql string, args ...any) ([]any, error) {
	rows, err := p.l.db.Query(ctx, sql, args...)
	if err != nil || len(rows.Values) == 0 {
		return nil, err
	}
	return rows.Values[0], nil
}

// get 取某条目某来源下指定角色的版本；role 为空按 current。
func (p *themedPages) get(ctx context.Context, itemID, kind, role string) (*jsObj, error) {
	if role == "" {
		role = "current"
	}
	r, err := p.oneRow(ctx, "SELECT "+themedVersionCols+" FROM themed_reading_versions WHERE item_id=? AND source_kind=? AND role=?", itemID, kind, role)
	if err != nil {
		return nil, err
	}
	return p.versionFromRow(r)
}

func (p *themedPages) getVersion(ctx context.Context, id string) (*jsObj, error) {
	r, err := p.oneRow(ctx, "SELECT "+themedVersionCols+" FROM themed_reading_versions WHERE id=?", id)
	if err != nil {
		return nil, err
	}
	return p.versionFromRow(r)
}

func (p *themedPages) assertWritable(ctx context.Context, itemID string) error {
	r, err := p.oneRow(ctx, "SELECT deleted_at FROM knowledge_items WHERE id=?", itemID)
	if err != nil {
		return err
	}
	if r == nil || r[0] != nil {
		return errors.New("条目不存在或已在回收站，不能继续生成主题阅读页")
	}
	return nil
}

func (p *themedPages) assertIdle(ctx context.Context, itemID, kind string) error {
	r, err := p.oneRow(ctx, "SELECT id FROM themed_reading_tasks WHERE item_id=? AND source_kind=? AND state IN ('queued','running') LIMIT 1", itemID, kind)
	if err != nil {
		return err
	}
	if r != nil {
		return errors.New("主题阅读页任务仍在执行，请先取消任务")
	}
	return nil
}

func (p *themedPages) listTasks(ctx context.Context) ([]*jsObj, error) {
	rows, err := p.l.db.Query(ctx, "SELECT "+themedTaskCols+" FROM themed_reading_tasks ORDER BY updated_at DESC,id DESC")
	if err != nil {
		return nil, err
	}
	out := make([]*jsObj, 0, len(rows.Values))
	for _, r := range rows.Values {
		t, err := p.taskFromRow(r)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

func (p *themedPages) getTask(ctx context.Context, id string) (*jsObj, error) {
	r, err := p.oneRow(ctx, "SELECT "+themedTaskCols+" FROM themed_reading_tasks WHERE id=?", id)
	if err != nil {
		return nil, err
	}
	return p.taskFromRow(r)
}

func themedJSON(v any) string        { s, _ := jsStringify(v); return s }
func themedMillis(v any) int64       { return int64(jsToNumber(v)) }
func themedActive(state string) bool { return state == "queued" || state == "running" }

// taskStatements 校验并生成保存任务所需的写语句（读取与判断在这里完成）。
func (p *themedPages) taskStatements(ctx context.Context, task *jsObj) ([]sqlite.Statement, error) {
	if err := themedValidate("validateTask", themedJSON(task)); err != nil {
		return nil, err
	}
	existing, err := p.getTask(ctx, themedStr(task, "id"))
	if err != nil {
		return nil, err
	}
	if existing != nil && (themedStr(existing, "itemId") != themedStr(task, "itemId") ||
		themedStr(existing, "sourceKind") != themedStr(task, "sourceKind") ||
		themedStr(existing, "versionId") != themedStr(task, "versionId")) {
		return nil, errors.New("不能更改主题阅读页任务的归属")
	}
	if themedActive(themedStr(task, "state")) {
		if err := p.assertWritable(ctx, themedStr(task, "itemId")); err != nil {
			return nil, err
		}
	}
	return []sqlite.Statement{{SQL: `INSERT INTO themed_reading_tasks (id,item_id,source_kind,version_id,state,payload,created_at,updated_at)
      VALUES (?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET state=excluded.state,payload=excluded.payload,updated_at=excluded.updated_at`,
		Args: []any{themedStr(task, "id"), themedStr(task, "itemId"), themedStr(task, "sourceKind"), themedStr(task, "versionId"),
			themedStr(task, "state"), themedJSON(task), themedMillis(task.get("createdAt")), themedMillis(task.get("updatedAt"))}}}, nil
}

func (p *themedPages) saveTask(ctx context.Context, task *jsObj) (*jsObj, error) {
	p.l.writeMu.Lock()
	defer p.l.writeMu.Unlock()
	stmts, err := p.taskStatements(ctx, task)
	if err != nil {
		return nil, err
	}
	if _, err := p.l.db.Transaction(ctx, stmts); err != nil {
		return nil, err
	}
	return p.getTask(ctx, themedStr(task, "id"))
}

// saveVersion 保存版本（及同一提交里的任务检查点）。
func (p *themedPages) saveVersion(ctx context.Context, version, task *jsObj) (*jsObj, error) {
	if err := themedValidate("validateVersion", themedJSON(version)); err != nil {
		return nil, err
	}
	if task != nil {
		if err := themedValidate("validateTask", themedJSON(task)); err != nil {
			return nil, err
		}
		if themedStr(task, "versionId") != themedStr(version, "id") || themedStr(task, "itemId") != themedStr(version, "itemId") ||
			themedStr(task, "sourceKind") != themedStr(version, "sourceKind") {
			return nil, errors.New("主题检查点与任务归属不一致")
		}
	}
	p.l.writeMu.Lock()
	defer p.l.writeMu.Unlock()
	itemID, kind, role, id := themedStr(version, "itemId"), themedStr(version, "sourceKind"), themedStr(version, "role"), themedStr(version, "id")
	if err := p.assertWritable(ctx, itemID); err != nil {
		return nil, err
	}
	existing, err := p.getVersion(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing != nil && (themedStr(existing, "itemId") != itemID || themedStr(existing, "sourceKind") != kind || themedStr(existing, "role") != role) {
		return nil, errors.New("不能更改主题阅读页的归属或直接切换版本角色")
	}
	var stmts []sqlite.Statement
	replaced, err := p.get(ctx, itemID, kind, role)
	if err != nil {
		return nil, err
	}
	if replaced != nil && themedStr(replaced, "id") != id {
		if role != "working" {
			return nil, errors.New("已发布版本必须通过发布操作替换")
		}
		if err := p.assertIdle(ctx, itemID, kind); err != nil {
			return nil, err
		}
		stmts = append(stmts, sqlite.Statement{SQL: "DELETE FROM themed_reading_versions WHERE id=?", Args: []any{themedStr(replaced, "id")}})
	}
	stmts = append(stmts,
		sqlite.Statement{SQL: `INSERT INTO themed_reading_versions (id,item_id,source_kind,role,payload,created_at,updated_at)
        VALUES (?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload,updated_at=excluded.updated_at`,
			Args: []any{id, itemID, kind, role, themedJSON(version), themedMillis(version.get("createdAt")), themedMillis(version.get("updatedAt"))}},
		sqlite.Statement{SQL: "DELETE FROM themed_reading_assets WHERE version_id=?", Args: []any{id}})
	type assetRef struct {
		sha string
		n   int64
	}
	var order []string
	assets := map[string]assetRef{}
	list, _ := version.get("assets").(jsArr)
	for _, a := range list {
		name := themedStr(a, "fileName")
		if name == "" {
			continue
		}
		sha := themedStr(a, "sha256")
		n := int64(0)
		if v := jsGet(a, "bytes"); v != jsUndefined {
			n = themedMillis(v)
		}
		prev, seen := assets[name]
		if seen && prev.sha != "" && sha != "" && (prev.sha != sha || prev.n != n) {
			return nil, errors.New("主题阅读页同名资源校验信息不一致")
		}
		// 尚未读取字节的原图也要保留引用，空摘要表示待校验而非零字节文件。
		if !seen || prev.sha == "" {
			if !seen {
				order = append(order, name)
			}
			assets[name] = assetRef{sha, n}
		}
	}
	for _, name := range order {
		a := assets[name]
		stmts = append(stmts, sqlite.Statement{SQL: "INSERT INTO themed_reading_assets VALUES (?,?,?,?)", Args: []any{id, name, a.sha, a.n}})
	}
	if task != nil {
		ts, err := p.taskStatements(ctx, task)
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, ts...)
	}
	if _, err := p.l.db.Transaction(ctx, stmts); err != nil {
		return nil, err
	}
	return p.getVersion(ctx, id)
}

func (p *themedPages) setRoleStatement(v *jsObj, role string) (sqlite.Statement, *jsObj) {
	next := newObj()
	for _, k := range v.keys() {
		next.set(k, v.get(k))
	}
	now := p.now()
	next.set("role", role)
	next.set("updatedAt", float64(now))
	return sqlite.Statement{SQL: "UPDATE themed_reading_versions SET role=?,payload=?,updated_at=? WHERE id=?",
		Args: []any{role, themedJSON(next), now, themedStr(v, "id")}}, next
}

// publish 把工作版本发布为 current，原 current 降为 previous。
func (p *themedPages) publish(ctx context.Context, versionID string, task *jsObj) (*jsObj, error) {
	p.l.writeMu.Lock()
	defer p.l.writeMu.Unlock()
	v, err := p.getVersion(ctx, versionID)
	if err != nil || v == nil {
		return nil, err
	}
	if task != nil {
		st := themedStr(task, "state")
		if themedStr(task, "versionId") != themedStr(v, "id") || themedStr(task, "itemId") != themedStr(v, "itemId") ||
			themedStr(task, "sourceKind") != themedStr(v, "sourceKind") || (st != "completed" && st != "partial") {
			return nil, errors.New("发布任务与页面不一致")
		}
	}
	itemID, kind := themedStr(v, "itemId"), themedStr(v, "sourceKind")
	if err := p.assertWritable(ctx, itemID); err != nil {
		return nil, err
	}
	if themedStr(v, "role") != "working" || !vTruthy(v.get("design")) {
		return nil, errors.New("只能发布已完成设计的工作版本")
	}
	latest, err := p.oneRow(ctx, "SELECT state FROM themed_reading_tasks WHERE version_id=? ORDER BY updated_at DESC,rowid DESC LIMIT 1", versionID)
	if err != nil {
		return nil, err
	}
	if latest != nil {
		if s, _ := latest[0].(string); s != "running" && s != "completed" && s != "partial" {
			return nil, errors.New("任务已停止，不能发布迟到的主题阅读页")
		}
	}
	current, err := p.get(ctx, itemID, kind, "current")
	if err != nil {
		return nil, err
	}
	stmts := []sqlite.Statement{{SQL: "DELETE FROM themed_reading_versions WHERE item_id=? AND source_kind=? AND role='previous'", Args: []any{itemID, kind}}}
	if current != nil {
		s, _ := p.setRoleStatement(current, "previous")
		stmts = append(stmts, s)
	}
	s, _ := p.setRoleStatement(v, "current")
	stmts = append(stmts, s)
	if task != nil {
		ts, err := p.taskStatements(ctx, task)
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, ts...)
	}
	if _, err := p.l.db.Transaction(ctx, stmts); err != nil {
		return nil, err
	}
	return p.getVersion(ctx, versionID)
}

// restorePrevious 交换 current 与 previous。
func (p *themedPages) restorePrevious(ctx context.Context, itemID, kind string) (*jsObj, error) {
	p.l.writeMu.Lock()
	defer p.l.writeMu.Unlock()
	if err := p.assertWritable(ctx, itemID); err != nil {
		return nil, err
	}
	if err := p.assertIdle(ctx, itemID, kind); err != nil {
		return nil, err
	}
	previous, err := p.get(ctx, itemID, kind, "previous")
	if err != nil {
		return nil, err
	}
	current, err := p.get(ctx, itemID, kind, "current")
	if err != nil {
		return nil, err
	}
	if previous == nil {
		return nil, nil
	}
	var stmts []sqlite.Statement
	// SQLite 唯一约束逐行检查；暂时清空角色，批次提交前完整恢复。
	if current != nil {
		stmts = append(stmts, sqlite.Statement{SQL: "UPDATE themed_reading_versions SET role=NULL WHERE id=?", Args: []any{themedStr(current, "id")}})
	}
	s, _ := p.setRoleStatement(previous, "current")
	stmts = append(stmts, s)
	if current != nil {
		s, _ := p.setRoleStatement(current, "previous")
		stmts = append(stmts, s)
	}
	if _, err := p.l.db.Transaction(ctx, stmts); err != nil {
		return nil, err
	}
	return p.getVersion(ctx, themedStr(previous, "id"))
}

// remove 删除某条目某来源下的全部版本，返回被引用过的资产文件名供孤儿清理。
func (p *themedPages) remove(ctx context.Context, itemID, kind string) ([]string, error) {
	p.l.writeMu.Lock()
	defer p.l.writeMu.Unlock()
	if err := p.assertIdle(ctx, itemID, kind); err != nil {
		return nil, err
	}
	rows, err := p.l.db.Query(ctx, `SELECT DISTINCT a.file_name FROM themed_reading_assets a JOIN themed_reading_versions v
        ON v.id=a.version_id WHERE v.item_id=? AND v.source_kind=?`, itemID, kind)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, r := range rows.Values {
		if s, ok := r[0].(string); ok {
			files = append(files, s)
		}
	}
	if _, err := p.l.db.Transaction(ctx, []sqlite.Statement{{SQL: "DELETE FROM themed_reading_versions WHERE item_id=? AND source_kind=?", Args: []any{itemID, kind}}}); err != nil {
		return nil, err
	}
	return files, nil
}

// interruptRunning 启动时把遗留的排队 / 运行任务标记为中断。
func (p *themedPages) interruptRunning(ctx context.Context) ([]*jsObj, error) {
	tasks, err := p.listTasks(ctx)
	if err != nil {
		return nil, err
	}
	var out []*jsObj
	for _, t := range tasks {
		if !themedActive(themedStr(t, "state")) {
			continue
		}
		next := newObj()
		for _, k := range t.keys() {
			next.set(k, t.get(k))
		}
		next.set("state", "interrupted")
		next.set("error", "应用运行已中断；已完成的图片已保留，请手动继续")
		next.set("updatedAt", float64(p.now()))
		saved, err := p.saveTask(ctx, next)
		if err != nil {
			return nil, err
		}
		out = append(out, saved)
	}
	return out, nil
}

func (p *themedPages) hasActiveTasks(ctx context.Context) (bool, error) {
	r, err := p.oneRow(ctx, "SELECT id FROM themed_reading_tasks WHERE state IN ('queued','running') LIMIT 1")
	return r != nil, err
}

// 旧格式（v1/v2）页面只读保留：可以阅读、导出和备份恢复，不再生成、补图或续跑。
const (
	themedLegacyPageError = "旧格式阅读页只读，不能调整、补图或继续生成；请重新生成新版阅读页"
	themedLegacyTaskError = "旧格式任务不再支持继续，请重新生成新版阅读页"
)

// retireLegacyTasks 页面数据保持原样；只把旧格式任务标记为不可继续，未完成的工作任务写明失败原因。
func (p *themedPages) retireLegacyTasks(ctx context.Context) {
	tasks, err := p.listTasks(ctx)
	if err != nil {
		p.l.paths.logAppError("themedReading", "retireLegacyTasks", err.Error(), nil)
		return
	}
	for _, task := range tasks {
		state := themedStr(task, "state")
		if jsGet(task, "legacyFormat") == true || (state != "interrupted" && state != "failed" && state != "cancelled" && state != "partial") {
			continue
		}
		err := func() error {
			version, err := p.getVersion(ctx, themedStr(task, "versionId"))
			if err != nil || version == nil || jsToNumber(version.get("formatVersion")) >= 3 {
				return err
			}
			next := themedCopyObj(task)
			next.set("legacyFormat", true)
			if themedStr(version, "role") == "working" {
				next.set("state", "failed")
				next.set("error", themedLegacyTaskError)
			}
			_, err = p.saveTask(ctx, next)
			return err
		}()
		if err != nil {
			// 单条存量记录损坏不能阻止阅读服务启动；继续时会再次报出具体原因。
			extra := newObj()
			extra.set("itemId", task.get("itemId"))
			p.l.paths.logAppError("themedReading", "retireLegacyTask", err.Error(), extra)
		}
	}
}

// startup 对应 ThemedReadingRuntime 构造函数：把遗留的排队 / 运行任务标记为中断，并清理旧格式任务。
func (p *themedPages) startup(ctx context.Context) {
	if _, err := p.interruptRunning(ctx); err != nil {
		p.l.paths.logAppError("themedReading", "interruptRunning", err.Error(), nil)
	}
	p.retireLegacyTasks(ctx)
}
