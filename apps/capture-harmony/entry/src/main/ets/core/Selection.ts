// 多选：只操作编号数组，不修改传入的数组（页面状态靠替换整个数组来刷新）。

// 已选则取消，未选则加入末尾。
export function toggleId(ids: string[], id: string): string[] {
  if (ids.includes(id)) return ids.filter((value: string): boolean => value !== id);
  return ids.concat([id]);
}

// 只保留仍然存在的编号：条目被发送、删除或撤销后，选择集合自动收缩，不会指向已不存在的草稿。
export function keepExisting(ids: string[], existing: string[]): string[] {
  return ids.filter((id: string): boolean => existing.includes(id));
}

// 全部可选条目都已被选中（没有可选条目时为 false，避免“全选”按钮在空列表上显示为已全选）。
export function allSelected(ids: string[], existing: string[]): boolean {
  if (existing.length === 0) return false;
  for (let i = 0; i < existing.length; i++) {
    if (!ids.includes(existing[i])) return false;
  }
  return true;
}
