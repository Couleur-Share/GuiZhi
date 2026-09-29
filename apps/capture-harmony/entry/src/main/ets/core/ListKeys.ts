import { Draft } from './Protocol';
import { DayGroup } from './DayGroups';

// ForEach 对键不变的项不会用新数据重建子项（父组件状态变化只更新组件参数，而参数里来自列表项自身的字段仍是首次创建时的旧值）。
// 因此凡是会影响显示的字段都要参与键，否则发送失败后的错误原因、“待确认”状态会停留在旧画面上。
export function draftKey(draft: Draft): string {
  return [draft.requestId, draft.mode, draft.attempted ? '1' : '0', draft.lastError].join('|');
}

// 分组的键：标签加各成员的键。成员变化（新增、删除、状态推进）时整个分组重建；成员都没变化则原样复用。
export function groupKey<T>(group: DayGroup<T>, keyOf: (item: T) => string): string {
  const keys: string[] = [];
  for (let i = 0; i < group.items.length; i++) keys.push(keyOf(group.items[i]));
  return `${group.label}#${keys.join(',')}`;
}
