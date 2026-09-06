import type { ImportTaskClearQuery, ImportTaskListQuery } from "@guizhi/shared/types";

/** 复用投递关联识别历史手机任务，不返回中继地址、投递凭证等内部信息。 */
const MOBILE_TASK = "EXISTS (SELECT 1 FROM mobile_capture_tasks m WHERE m.task_id = import_tasks.id)";
export const TASK_SELECT = `SELECT import_tasks.*,
  CASE WHEN ${MOBILE_TASK} THEN 'mobile' ELSE 'desktop' END AS submitted_from,
  (SELECT r.created_at FROM mobile_capture_tasks m JOIN mobile_capture_receipts r
   ON r.relay_key = m.relay_key AND r.delivery_id = m.delivery_id
   WHERE m.task_id = import_tasks.id) AS received_at FROM import_tasks`;

export function taskSearch(query: Pick<ImportTaskListQuery, "query" | "origin">) {
  if (query.origin !== undefined && !["all", "mobile", "desktop"].includes(query.origin)) {
    throw new Error("提交来源筛选不合法");
  }
  const keyword = query.query?.trim().toLowerCase();
  const pattern = `%${keyword?.replace(/[\\%_]/g, "\\$&")}%`;
  const source = query.origin === "mobile" ? ` AND ${MOBILE_TASK}`
    : query.origin === "desktop" ? ` AND NOT ${MOBILE_TASK}` : "";
  return {
    sql: source + (keyword ? ` AND (LOWER(display_name) LIKE ? ESCAPE '\\' OR LOWER(source_input) LIKE ? ESCAPE '\\'
      OR LOWER(COALESCE(error, '')) LIKE ? ESCAPE '\\' OR LOWER(COALESCE(warning, '')) LIKE ? ESCAPE '\\')` : ""),
    params: keyword ? [pattern, pattern, pattern, pattern] : [],
  };
}

export function taskStatus(status: ImportTaskListQuery["status"]) {
  if (!status || status === "all") return { sql: "", params: [] };
  if (status === "active") return { sql: " AND status IN ('pending','processing')", params: [] };
  if (status === "degraded") return { sql: " AND status = 'completed' AND COALESCE(warning, '') <> ''", params: [] };
  return { sql: " AND status = ?", params: [status] };
}

/** 预览和删除使用同一条件；进行中筛选不会意外扩大为全部终态。 */
export function terminalFilter(query: ImportTaskClearQuery) {
  if (!query || !["filtered", "all"].includes(query.scope)) throw new Error("清理范围不合法");
  const filtered: ImportTaskListQuery = query.scope === "filtered" ? query : {};
  const search = taskSearch(filtered);
  const status = taskStatus(filtered.status);
  return {
    sql: `status IN ('completed','failed','canceled','duplicate')${search.sql}${status.sql}`,
    params: [...search.params, ...status.params],
  };
}
