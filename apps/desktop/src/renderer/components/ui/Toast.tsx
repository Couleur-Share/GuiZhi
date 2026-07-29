import { useState, useEffect, createContext, useContext, useCallback, useRef } from 'react';
import { createPortal } from 'react-dom';
import { useTranslation } from 'react-i18next';
import {
  CheckCircleIcon,
  XCircleIcon,
  InfoIcon,
  AlertTriangleIcon,
  ChevronRightIcon,
  CopyIcon,
  XIcon,
} from 'lucide-react';
import { useSettingsStore } from '../../stores/settings.store';
import { MOTION_DURATION } from '../../styles/motion-tokens';
import { copyTextToClipboard } from '../../utils/clipboard';

// Toast type
// Toast 类型
type ToastType = 'success' | 'error' | 'info' | 'warning';

interface ToastAction {
  label: string;
  onClick: () => void;
}

export interface ShowToastOptions {
  /**
   * 完整报错原文，折叠在「查看详情」后面。
   *
   * 提示语要短才读得进去，但「失败」两个字对排查毫无用处——原因收在这里，
   * 展开能读、能复制去搜。批量操作把逐条失败明细放这儿最合适。
   */
  detail?: string;
  /** 同时发一条系统通知（需在设置里开启通知） */
  systemNotification?: boolean;
}

interface Toast {
  id: string;
  message: string;
  type: ToastType;
  detail?: string;
  /** 行内动作按钮（撤销等）；点击后 toast 立即收起 */
  action?: ToastAction;
  /** 覆盖默认的自动消失时长 */
  dismissAfterMs?: number;
  /**
   * When true, the toast is in its exit animation. The DOM node remains
   * mounted for one duration-quick window so the fade-out / slide-out
   * classes have time to play before unmount.
   * 标记为 true 时，该 toast 处于退出动画阶段；DOM 节点会再保留一个
   * duration-quick 窗口让退场动画播完，然后才真正卸载。
   */
  leaving?: boolean;
}

interface ToastContextType {
  showToast: (
    message: string,
    type?: ToastType,
    options?: ShowToastOptions,
  ) => void;
  /**
   * 带「撤销」按钮的提示。
   *
   * 删除这类动作不该先弹确认框打断操作，但也不能删完毫无反应——
   * 事后给一个撤销窗口，误删 200 条不必再去回收站逐条勾选恢复。
   */
  showUndoToast: (message: string, onUndo: () => void) => void;
}

const ToastContext = createContext<ToastContextType | null>(null);

// Success / info toasts fade out on their own; errors and warnings stay until
// the user closes them. A failure that disappears after three seconds is a
// failure the user never sees — they just retry the same action.
// 成功/提示类自动淡出；错误与警告留到用户手动关闭。几秒就消失的报错等于没报，
// 用户只会把同一个操作再做一遍。
const AUTO_DISMISS_MS = 3000;
/** 撤销窗口：3 秒不够读完一句话再决定要不要点 */
const UNDO_DISMISS_MS = 8000;
// Persistent toasts must not be able to fill the screen; keep the newest few.
// 不自动消失就得有上限，否则堆满屏幕；只保留最新的几条。
const MAX_VISIBLE_TOASTS = 5;

function isPersistent(type: ToastType): boolean {
  return type === 'error' || type === 'warning';
}

/**
 * 超出上限时优先挤掉最老的「会自己消失」的那条。
 *
 * 一律砍最老的话，批量导入 10 个文件全失败时，前 5 条错误会在用户读到之前
 * 就被后来的挤没——而它们恰恰是不该消失的那类。
 */
function trimToasts(list: Toast[]): Toast[] {
  if (list.length <= MAX_VISIBLE_TOASTS) {
    return list;
  }
  const transientIndex = list.findIndex((toast) => !isPersistent(toast.type));
  const dropIndex = transientIndex === -1 ? 0 : transientIndex;
  return list.filter((_, index) => index !== dropIndex);
}

/**
 * 折叠起来的报错原文。
 *
 * 展开而不是直接铺开：提示语要短才有人读，但原因不能因此丢掉。
 * 「复制」是这一块的重点——用户拿到原文才谈得上去搜或者提 issue。
 */
function ToastDetail({ detail }: { detail: string }) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(false);
  const [copied, setCopied] = useState(false);
  const copiedTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(
    () => () => {
      if (copiedTimer.current) {
        clearTimeout(copiedTimer.current);
      }
    },
    [],
  );

  const copy = async () => {
    try {
      await copyTextToClipboard(detail);
      setCopied(true);
      copiedTimer.current = setTimeout(() => setCopied(false), 2000);
    } catch {
      // 复制不了也没关系：原文就摊在上面，用户照样读得到
    }
  };

  return (
    <div className="mt-1.5">
      <button
        type="button"
        onClick={() => setExpanded((prev) => !prev)}
        aria-expanded={expanded}
        className="inline-flex items-center gap-0.5 rounded-md text-xs font-medium text-muted-foreground transition-colors hover:text-foreground"
      >
        <ChevronRightIcon
          aria-hidden="true"
          className={`h-3.5 w-3.5 transition-transform ${expanded ? 'rotate-90' : ''}`}
        />
        {expanded ? t('common.collapse', '收起') : t('common.viewDetail', '查看详情')}
      </button>
      {expanded ? (
        <div className="mt-1.5 rounded-lg bg-black/5 p-2 dark:bg-white/5">
          <pre className="max-h-40 overflow-y-auto whitespace-pre-wrap break-words font-mono text-[11px] font-normal leading-relaxed text-muted-foreground">
            {detail}
          </pre>
          <button
            type="button"
            onClick={() => void copy()}
            className="mt-1.5 inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[11px] font-medium text-muted-foreground transition-colors hover:bg-black/5 hover:text-foreground dark:hover:bg-white/10"
          >
            <CopyIcon aria-hidden="true" className="h-3 w-3" />
            {copied ? t('toast.copied', '已复制') : t('common.copy', '复制')}
          </button>
        </div>
      ) : null}
    </div>
  );
}

// Toast Provider
export function ToastProvider({ children }: { children: React.ReactNode }) {
  const { t } = useTranslation();
  const [toasts, setToasts] = useState<Toast[]>([]);
  const enableNotifications = useSettingsStore((state) => state.enableNotifications);
  // Track timers so React strict-mode double-mount and rapid replacements
  // do not orphan setTimeout callbacks. Indexed by toast id.
  // 用 ref 记录定时器，避免 React strict-mode 双挂载与快速替换造成游离
  // setTimeout；以 toast id 为 key。
  const exitTimers = useRef<Map<string, ReturnType<typeof setTimeout>>>(new Map());
  const autoDismissTimers = useRef<Map<string, ReturnType<typeof setTimeout>>>(new Map());
  // Monotonic counter so two showToast calls inside the same millisecond
  // still get distinct ids (Date.now().toString() alone collides under
  // batched user actions).
  // 单调递增计数器，避免同一毫秒内连续 showToast 出现重复 id 与 React key
  // 冲突（仅靠 Date.now() 在批量操作下会撞 id）。
  const idCounter = useRef(0);

  const removeToast = useCallback((id: string) => {
    // Clear any pending auto-dismiss so we do not double-fire removeToast.
    // 清掉自动消失的定时器，避免之后再次触发 removeToast。
    const auto = autoDismissTimers.current.get(id);
    if (auto) {
      clearTimeout(auto);
      autoDismissTimers.current.delete(id);
    }
    // Already leaving? Don't restart the exit animation timer.
    // 已经处于退场状态就不再重置定时器，避免动画抖动。
    if (exitTimers.current.has(id)) return;
    setToasts((prev) =>
      prev.map((toast) => (toast.id === id ? { ...toast, leaving: true } : toast)),
    );
    const timer = setTimeout(() => {
      setToasts((prev) => prev.filter((toast) => toast.id !== id));
      exitTimers.current.delete(id);
    }, MOTION_DURATION.quick + 20); // small slack so the animation finishes
    exitTimers.current.set(id, timer);
  }, []);

  const showToast = useCallback((message: string, type: ToastType = 'success', options?: ShowToastOptions) => {
    idCounter.current += 1;
    const id = `${Date.now()}-${idCounter.current}`;
    setToasts((prev) =>
      trimToasts([...prev, { id, message, type, detail: options?.detail }]),
    );

    // Send system notification (if enabled and requested)
    // 发送系统通知（如果启用且请求）
    if (options?.systemNotification && enableNotifications && window.electron?.showNotification) {
      const title =
        type === 'success'
          ? t('common.success', 'Success')
          : type === 'error'
            ? t('common.error', 'Error')
            : type === 'warning'
              ? t('common.warning', 'Warning')
              : t('common.info', 'Info');
      void window.electron.showNotification(`GuiZhi - ${title}`, message);
    }

    if (isPersistent(type)) {
      return;
    }

    // Auto-dismiss via the same exit-animation pipeline.
    // 通过同一个退场动画管线自动消失。
    const dismiss = setTimeout(() => {
      autoDismissTimers.current.delete(id);
      removeToast(id);
    }, AUTO_DISMISS_MS);
    autoDismissTimers.current.set(id, dismiss);
  }, [enableNotifications, removeToast, t]);

  const showUndoToast = useCallback((message: string, onUndo: () => void) => {
    idCounter.current += 1;
    const id = `${Date.now()}-${idCounter.current}`;
    setToasts((prev) =>
      trimToasts([
        ...prev,
        {
          id,
          message,
          type: 'info',
          action: {
            label: t('common.undo', '撤销'),
            onClick: onUndo,
          },
        },
      ]),
    );
    const dismiss = setTimeout(() => {
      autoDismissTimers.current.delete(id);
      removeToast(id);
    }, UNDO_DISMISS_MS);
    autoDismissTimers.current.set(id, dismiss);
  }, [removeToast, t]);

  // Clean up timers on unmount.
  useEffect(() => {
    const exit = exitTimers.current;
    const auto = autoDismissTimers.current;
    return () => {
      exit.forEach((timer) => clearTimeout(timer));
      exit.clear();
      auto.forEach((timer) => clearTimeout(timer));
      auto.clear();
    };
  }, []);

  const getIcon = (type: ToastType) => {
    switch (type) {
      case 'success':
        return <CheckCircleIcon aria-hidden="true" className="w-5 h-5 text-green-500" />;
      case 'error':
        return <XCircleIcon aria-hidden="true" className="w-5 h-5 text-red-500" />;
      case 'warning':
        return <AlertTriangleIcon aria-hidden="true" className="w-5 h-5 text-yellow-500" />;
      case 'info':
      default:
        return <InfoIcon aria-hidden="true" className="w-5 h-5 text-blue-500" />;
    }
  };

  const getBgColor = (type: ToastType) => {
    switch (type) {
      case 'success':
        return 'bg-green-50 dark:bg-green-900/20 border-green-200 dark:border-green-800';
      case 'error':
        return 'bg-red-50 dark:bg-red-900/20 border-red-200 dark:border-red-800';
      case 'warning':
        return 'bg-yellow-50 dark:bg-yellow-900/20 border-yellow-200 dark:border-yellow-800';
      case 'info':
      default:
        return 'bg-blue-50 dark:bg-blue-900/20 border-blue-200 dark:border-blue-800';
    }
  };

  return (
    <ToastContext.Provider value={{ showToast, showUndoToast }}>
      {children}

      {/* Toast container - z-index needs to be the highest to stay above everything */}
      {/* Toast 容器 - z-index 需要最高，确保在所有元素之上 */}
      {createPortal(
        <div className="fixed bottom-6 right-6 z-[99999] flex flex-col gap-3 pointer-events-none">
          {toasts.map((toast) => (
            <div
              key={toast.id}
              className={`
                px-4 py-3 rounded-2xl border shadow-2xl pointer-events-auto
                max-w-[26rem]
                ${
                  toast.leaving
                    ? 'animate-out slide-out-to-right-10 fade-out duration-quick ease-exit'
                    : 'animate-in slide-in-from-right-10 fade-in duration-base ease-enter'
                }
                backdrop-blur-md
                ${getBgColor(toast.type)}
              `}
            >
              {/*
                主行用 items-center：关闭按钮比文字高，items-start 会把图标和
                文案顶到上沿，底下多出一截空白，看起来就是上窄下宽。
                详情挂在主行下面，展开时不会把图标/关闭钮拽到整块正中间。
              */}
              <div className="flex items-center gap-3">
                <span className="shrink-0">{getIcon(toast.type)}</span>
                <p className="min-w-0 flex-1 text-sm font-semibold leading-snug text-foreground break-words">
                  {toast.message}
                </p>
                {toast.action ? (
                  <button
                    type="button"
                    onClick={() => {
                      toast.action?.onClick();
                      removeToast(toast.id);
                    }}
                    data-testid="toast-action"
                    className="shrink-0 rounded-lg px-2 py-1 text-sm font-semibold text-primary transition-colors hover:bg-primary/10"
                  >
                    {toast.action.label}
                  </button>
                ) : null}
                <button
                  type="button"
                  onClick={() => removeToast(toast.id)}
                  className="shrink-0 rounded-lg p-1 hover:bg-black/10 dark:hover:bg-white/10 transition-colors"
                  aria-label={t('common.close', 'Close')}
                >
                  <XIcon aria-hidden="true" className="w-4 h-4 text-muted-foreground" />
                </button>
              </div>
              {toast.detail ? (
                <div className="pl-8">
                  <ToastDetail detail={toast.detail} />
                </div>
              ) : null}
            </div>
          ))}
        </div>,
        document.body
      )}
    </ToastContext.Provider>
  );
}

// Hook
// Hook
export function useToast() {
  const context = useContext(ToastContext);
  if (!context) {
    throw new Error('useToast must be used within a ToastProvider');
  }
  return context;
}
