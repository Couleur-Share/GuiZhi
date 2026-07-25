/**
 * 自动检查更新的触发判定。
 *
 * 抽成纯函数是为了能被单测覆盖：这段判断此前内联在 App.tsx 的 useEffect 里，
 * 且跳过时既不落日志也无提示——「没检查」与「检查了但没有新版本」在界面上
 * 完全同形，用户只能得出「自动检查更新没生效」的结论。
 */

/** 自动检查的触发来源；手动检查走更新弹窗，不经过这里 */
export type AutoUpdateCheckTrigger = "startup" | "interval" | "visibility";

/** 跳过原因；返回 null 表示应当真的发起一次检查 */
export type AutoUpdateSkipReason =
  | "disabled"
  | "hidden"
  | "offline"
  | "in-flight"
  | "cooldown";

/** 周期检查间隔 */
export const AUTO_UPDATE_CHECK_INTERVAL_MS = 60 * 60 * 1000;

/** 上次检查失败后，允许提前重试的最小间隔 */
export const AUTO_UPDATE_RETRY_INTERVAL_MS = 5 * 60 * 1000;

export interface AutoUpdateCheckContext {
  trigger: AutoUpdateCheckTrigger;
  enabled: boolean;
  windowVisible: boolean;
  online: boolean;
  inFlight: boolean;
  now: number;
  /** 上次实际发起检查的时间戳；0 表示本次会话还没检查过 */
  lastAttemptAt: number;
  lastAttemptFailed: boolean;
}

export function resolveAutoUpdateSkipReason(
  context: AutoUpdateCheckContext,
): AutoUpdateSkipReason | null {
  if (!context.enabled) {
    return "disabled";
  }
  if (!context.windowVisible) {
    return "hidden";
  }
  if (!context.online) {
    return "offline";
  }
  if (context.inFlight) {
    return "in-flight";
  }

  // 窗口每次显示都补检，会在托盘频繁切换时打成连击；按上次结果给冷却窗口，
  // 失败过就允许更早重试（否则一次网络抖动会静默吞掉整整一小时的更新）。
  if (context.trigger === "visibility" && context.lastAttemptAt > 0) {
    const cooldownMs = context.lastAttemptFailed
      ? AUTO_UPDATE_RETRY_INTERVAL_MS
      : AUTO_UPDATE_CHECK_INTERVAL_MS;
    if (context.now - context.lastAttemptAt < cooldownMs) {
      return "cooldown";
    }
  }

  return null;
}
