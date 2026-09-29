// 列表按“今天 / 昨天 / 更早”分组。按本机日历日划分（跨过午夜就换组），而不是“24 小时内”的滚动窗口。
export class DayGroup<T> {
  label: string = '';
  items: T[] = [];
}

export const TODAY_LABEL: string = '今天';
export const YESTERDAY_LABEL: string = '昨天';
export const EARLIER_LABEL: string = '更早';

// 本机时区当天 0 点的时间戳；用日历日期重新构造，夏令时切换日也不会错位。
function dayStart(timestamp: number): number {
  const date = new Date(timestamp);
  return new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime();
}

// 无法判断的时间（缺失、非数字）归入“更早”；晚于现在的时间（时钟偏差）按“今天”处理，不产生“明天”这种分组。
export function dayLabel(timestamp: number, now: number): string {
  if (!Number.isFinite(timestamp) || timestamp <= 0) return EARLIER_LABEL;
  const today = dayStart(now);
  const day = dayStart(timestamp);
  if (day >= today) return TODAY_LABEL;
  return day === dayStart(today - 1) ? YESTERDAY_LABEL : EARLIER_LABEL;
}

// 分组顺序固定为今天、昨天、更早，空分组不返回；组内保持传入顺序。
export function groupByDay<T>(items: T[], timeOf: (item: T) => number, now: number): DayGroup<T>[] {
  const labels: string[] = [TODAY_LABEL, YESTERDAY_LABEL, EARLIER_LABEL];
  const groups: DayGroup<T>[] = [];
  for (let i = 0; i < labels.length; i++) {
    const group = new DayGroup<T>();
    group.label = labels[i];
    groups.push(group);
  }
  for (let i = 0; i < items.length; i++) {
    const label = dayLabel(timeOf(items[i]), now);
    for (let j = 0; j < groups.length; j++) {
      if (groups[j].label === label) groups[j].items.push(items[i]);
    }
  }
  return groups.filter((group: DayGroup<T>): boolean => group.items.length > 0);
}
