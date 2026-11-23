/**
 * 主题阅读（AI 阅读，M3-5b，台账 B-064）对照场景的数据层：把一份固定的夹具装进真实的渲染层，让界面处在确定的数据里。
 *
 * 为什么不直接走真实生成：生成要真模型与联网搜索；任务的阶段与失败原因由主进程在后台推进，两次采集不可复现。
 * 这里在页内换掉 window.api.themedReading 的读取接口（渲染层的组件与 hook 每次调用都从 window.api 现取）：
 * getState / get / references / searchConfig / listTasks / preview，以及阅读岛的 createView 一族（页面本体由 WebView2 承载，不在无头对照范围内）。
 * 渲染层一行不改，台账「渲染层改动登记」保持为空。Viva 一侧用同一份夹具与同样的开关（viva/parity/themedfixture.go、viva/parity/scenes/themed.go）。
 *
 * 与 react-ask.mjs 的提问栏同一做法：页内替换的函数体 themedPatch 自包含，在场景选中条目之前直接在已加载的页面里执行（applyThemedPatch）；
 * 场景收尾的 leaveLibrary / leaveInbox 会 reload，把替换连同页面状态一起收走。
 */
import fs from "node:fs";
import path from "node:path";
import { parityLanguage } from "./react-state.mjs";

/** 夹具：与 Viva 场景共用同一份 JSON。 */
export const THEMED_FIXTURE = JSON.parse(
  fs.readFileSync(path.join(import.meta.dirname, "../../../viva/parity/testdata/themed-fixture.json"), "utf8"),
);

/** 渲染层 i18n 目录（zh.json / en.json）：场景要点的可访问名直接取自目录，两种界面语言不用手抄。 */
const LOCALES_DIR = path.join(import.meta.dirname, "../../../../desktop/src/renderer/i18n/locales");
const LOCALES = Object.fromEntries(["zh", "en"].map((lang) => [lang, JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, `${lang}.json`), "utf8"))]));

/** 目录里 scope.key 的文案，按 PARITY_LANG 取中文或英文；键写错时直接抛错（getByRole 等不到元素只会让采集超时）。 */
export function themedLabel(scope, key) {
  const value = LOCALES[parityLanguage()][scope]?.[key];
  if (typeof value !== "string") throw new Error(`i18n 目录里没有 ${scope}.${key}`);
  return value;
}

/**
 * 页内替换（在页内运行，必须自包含）：按夹具与配置改写 window.api.themedReading 里要读写的接口。
 * config：{ page（这篇内容有已生成的阅读页）, task（这篇内容最近的任务：夹具里的键名 running | failed，空表示没有）}。
 * 计数写到 window.__parityThemed，供采集端等「各接口都回过一次」。
 */
function themedPatch(api, fixture, config) {
  if (!api || !api.themedReading) return;
  const seen = (window.__parityThemed = window.__parityThemed || { state: 0, get: 0, list: 0 });
  const clone = (value) => (value === undefined ? undefined : JSON.parse(JSON.stringify(value)));
  const page = config.page ? fixture.page : null;
  const task = config.task ? fixture[config.task] : null;
  const ok = (extra) => Promise.resolve({ success: true, ...extra });
  Object.assign(api.themedReading, {
    __parity: true,
    getState: () => {
      seen.state += 1;
      return ok({
        state: clone({
          hasPage: !!page, versionId: page ? page.id : undefined, formatVersion: page ? page.formatVersion : undefined,
          stale: false, task: task || undefined,
        }),
      });
    },
    get: () => {
      seen.get += 1;
      return ok({
        page: clone(page), previous: false, stale: false, task: clone(task || undefined),
        models: { text: fixture.models.text, image: fixture.models.image }, search: clone(fixture.search),
      });
    },
    references: () => ok({ references: [] }),
    searchConfig: () => ok({ search: clone(fixture.search) }),
    listTasks: () => {
      seen.list += 1;
      return ok({ tasks: clone(fixture.inbox.tasks) });
    },
    preview: () => ok({}),
    // 阅读岛（页面本体由 WebView2 承载）：给一个视图号，其余命令都应答成功、不推事件。
    createView: () => ok({ viewId: "parity-view" }),
    updateView: () => ok({}),
    commandView: () => ok({}),
    destroyView: () => ok({}),
    onViewEvent: () => () => {},
    // 采集不会写：万一被触发，应答失败（不落任何东西）。
    generate: () => Promise.resolve({ success: false, error: "parity" }),
    cancel: () => Promise.resolve({ success: false, error: "parity" }),
    resume: () => Promise.resolve({ success: false, error: "parity" }),
    continueOffline: () => Promise.resolve({ success: false, error: "parity" }),
    regenerateAsset: () => Promise.resolve({ success: false, error: "parity" }),
    restorePrevious: () => Promise.resolve({ success: false, error: "parity" }),
    remove: () => Promise.resolve({ success: false, error: "parity" }),
    exportHtml: () => Promise.resolve({ success: false, error: "parity" }),
    onProgress: () => () => {},
  });
}

/** 在已加载的页面里装上替换（要赶在选中条目、打开主题页任务之前）。 */
export async function applyThemedPatch(win, config) {
  await win.evaluate(`(${themedPatch.toString()})(window.api, ${JSON.stringify(THEMED_FIXTURE)}, ${JSON.stringify(config)})`);
}
