/**
 * 驱动渲染层真实的 ToastProvider，把提示摆成对照场景需要的状态。
 *
 * 为什么不走用户流程：提示是各处业务操作的副作用，没有一个确定的、不依赖外部资源的入口能弹出
 * 「带详情的错误」「撤销」这些形态。这里沿 React 在 DOM 节点上挂的 fiber 引用往上找 ToastProvider 的 context 值
 * （{ showToast, showUndoToast, dismissToast }，生产构建里同样存在）直接调用——驱动的仍是渲染层自己的组件，
 * 不改渲染层一行代码（台账「渲染层改动登记」保持为空）。
 *
 * 画面里只留提示：把 #root 设成 visibility:hidden（提示容器是 portal 到 body 的 fixed 元素，不受影响；
 * body 自己带 bg-background，所以提示背后是纯页面底色）。理由与取舍见台账 A-007：Viva 没有背景模糊，
 * 提示的 backdrop-blur-md 只在背后是纯色时才能逐像素对照；背后有内容的情形不在对照范围内（G-005）。
 */

/** 提示容器：fixed bottom-6 right-6 z-[99999]，body 的直接子元素。 */
export const TOAST_CONTAINER = "body > div.fixed.bottom-6.right-6";

/** 提示的两个自动消失时长（AUTO_DISMISS_MS、UNDO_DISMISS_MS）：场景里要冻住，否则采集到一半提示自己没了。 */
const AUTO_DISMISS_MS = 3000;
const UNDO_DISMISS_MS = 8000;

/**
 * 清掉留下的提示并恢复页面可见：每个提示场景离开时调用，也是进入场景前的幂等复位。
 * 靠点每条提示的关闭钮（唯一带 aria-label 的按钮）收走它们：showUndoToast 不返回 id，没法按 id 撤。
 */
export async function resetToasts(win) {
  await win.evaluate((sel) => {
    const root = document.getElementById("root");
    if (root) root.style.visibility = "";
    for (const button of document.querySelectorAll(`${sel} button[aria-label]`)) button.click();
  }, TOAST_CONTAINER);
  // 退场要 quick+20 ms 才卸载（定时器，SETTLE_CSS 关不掉）。
  await win.waitForFunction((sel) => !document.querySelector(sel)?.children.length, TOAST_CONTAINER, { timeout: 5000 });
}

/**
 * 按顺序弹出一组提示，页面内容隐去，等它们都出现在 DOM 里。
 * specs 的每项：{ message, type: "success"|"error"|"info"|"warning", detail? } 或 { message, undo: true }。
 */
export async function showToasts(win, specs) {
  await resetToasts(win);
  await win.evaluate(({ items, autoMs, undoMs }) => {
    // 沿 fiber 往上找 ToastProvider 的 context 值（不用 new Function：页面的 CSP 可能不允许）。
    const root = document.getElementById("root");
    const start = root && root.firstElementChild ? root.firstElementChild : root;
    const key = start && Object.keys(start).find((k) => k.startsWith("__reactFiber$"));
    if (!key) throw new Error("找不到 React fiber：渲染层的挂载方式变了？");
    let api = null;
    for (let f = start[key]; f && !api; f = f.return) {
      const v = f.memoizedProps && f.memoizedProps.value;
      if (v && typeof v.showToast === "function" && typeof v.showUndoToast === "function") api = v;
    }
    if (!api) throw new Error("沿 fiber 往上没有找到 ToastProvider 的 context 值");

    root.style.visibility = "hidden";
    // 渲染层在 showToast 里同步起自动消失的定时器：弹的这一刻把这两个时长的定时器换成永不触发的空操作，随后立即还原。
    const realSetTimeout = window.setTimeout;
    window.setTimeout = (fn, ms, ...rest) => (ms === autoMs || ms === undoMs ? 0 : realSetTimeout(fn, ms, ...rest));
    try {
      for (const item of items) {
        if (item.undo) api.showUndoToast(item.message, () => {});
        else api.showToast(item.message, item.type, item.detail ? { detail: item.detail } : undefined);
      }
    } finally {
      window.setTimeout = realSetTimeout;
    }
  }, { items: specs, autoMs: AUTO_DISMISS_MS, undoMs: UNDO_DISMISS_MS });
  await win.waitForFunction(({ sel, count }) => document.querySelector(sel)?.children.length === count, { sel: TOAST_CONTAINER, count: specs.length }, { timeout: 5000 });
}
