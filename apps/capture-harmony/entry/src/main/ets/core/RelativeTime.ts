// 相对时间文案：用于草稿创建时间与近期收集记录。now 由调用者传入，便于测试且不依赖系统时钟。
const MINUTE = 60000;
const HOUR = 3600000;
const DAY = 86400000;

function two(value: number): string { return value < 10 ? `0${value}` : `${value}`; }

function clockText(date: Date): string { return `${two(date.getHours())}:${two(date.getMinutes())}`; }

// 本地日历日序号：按日期分量计算，避免夏令时或跨零点时把“24 小时内”误判为同一天。
function dayIndex(date: Date): number {
  return Math.round(Date.UTC(date.getFullYear(), date.getMonth(), date.getDate()) / DAY);
}

function absoluteText(then: Date, current: Date): string {
  if (then.getFullYear() !== current.getFullYear()) {
    return `${then.getFullYear()}年${then.getMonth() + 1}月${then.getDate()}日`;
  }
  return `${then.getMonth() + 1}月${then.getDate()}日 ${clockText(then)}`;
}

export function relativeTime(timestamp: number, now: number): string {
  if (!(timestamp > 0)) return '';
  const diff = now - timestamp;
  const then = new Date(timestamp);
  const current = new Date(now);
  // 时间在未来：允许 5 分钟内的设备时钟偏差，更大的偏差直接显示绝对时间，不编造“几小时前”。
  if (diff < 0) return -diff <= 5 * MINUTE ? '刚刚' : absoluteText(then, current);
  if (diff < MINUTE) return '刚刚';
  if (diff < HOUR) return `${Math.floor(diff / MINUTE)} 分钟前`;
  const days = dayIndex(current) - dayIndex(then);
  if (days === 0) return `${Math.floor(diff / HOUR)} 小时前`;
  if (days === 1) return `昨天 ${clockText(then)}`;
  if (days === 2) return `前天 ${clockText(then)}`;
  return absoluteText(then, current);
}
