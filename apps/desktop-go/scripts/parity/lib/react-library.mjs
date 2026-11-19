/**
 * 知识库侧栏（library）对照场景的数据层：把一份固定的合集、标签与计数夹具装进真实的渲染层。
 *
 * 做法与采集队列一致（lib/react-imports.mjs）：不往库里写数据，而是在页内换掉 window.api 的读取接口——
 * collection.list、tag.list、knowledge.counts 返回夹具，变更接口只回成功。渲染层的组件与 store 一行不改，
 * 台账「渲染层改动登记」保持为空。Viva 一侧用同一份夹具（viva/parity/libraryfixture.go）。
 *
 * 计数与筛选无关：任何查询都返回同一份计数。真实库里分面计数会随筛选变化，但两侧输入相同，
 * 对照比较的是「同样的数据画成什么样」。
 *
 * 智能视图存在 localStorage（guizhi-library-smart-views-v1），由 enterLibrary 在 reload 之前写好、leaveLibrary 清掉。
 *
 * 工作区（M2-2 之前 Viva 还没有）整体隐去：只让 `侧栏旁边的内容容器` 里的内容 visibility:hidden，布局不变，
 * 内容区画面是页面底色，Viva 一侧对应一块空白工作区。这是采集端的口径（页内样式），不是渲染层源码的改动。
 * M2-2 做出列表之后，整窗场景再把工作区放回来比较。
 */
import fs from "node:fs";
import path from "node:path";
import { SETTLE_CSS, settle } from "./react-state.mjs";

/** 夹具：与 Viva 场景共用同一份 JSON。 */
export const LIBRARY_FIXTURE = JSON.parse(
  fs.readFileSync(path.join(import.meta.dirname, "../../../viva/parity/testdata/library-fixture.json"), "utf8"),
);

const CONFIG_KEY = "__parity.library";
const SMART_VIEWS_KEY = "guizhi-library-smart-views-v1";
const REGISTERED = Symbol("parity.library.registered");

/** 隐去工作区：侧栏面板（`aside`）紧邻的内容容器里的一切。选择器限定在「面板与内容并排」的那一层，不碰 rail 与顶栏。 */
const HIDE_WORKSPACE_CSS = `
div.relative.min-h-0.flex-1.overflow-hidden > aside + div > * { visibility: hidden !important; }
`;

/**
 * 页面初始化脚本（在页内运行，不能引用外层变量）：读配置，按配置替换 window.api 的合集、标签与计数接口。
 * 每个接口完成一次就记一笔到 window.__parityLibrary，场景据此知道侧栏已经拿到了数据（空库没有可等的文字）。
 */
function pageInit({ fixture, configKey }) {
  let config = null;
  try {
    config = JSON.parse(localStorage.getItem(configKey) || "null");
  } catch {
    return; // about:blank 等拿不到 localStorage 的文档
  }
  if (!config) return;

  const data = fixture.variants[config.variant];
  if (!data) return;
  // 变更要在页内生效（删除合集之后列表里少一行），所以复制一份可改的。
  const collections = data.collections.map((c) => ({ ...c }));
  const tags = data.tags.map((t) => ({ ...t }));
  const seen = (window.__parityLibrary = { collections: 0, tags: 0, counts: 0 });
  const done = (key, value) => { seen[key] += 1; return Promise.resolve(value); };

  const patch = (api) => {
    if (!api || !api.collection || api.collection.__parity) return;
    Object.assign(api.collection, {
      __parity: true,
      list: () => done("collections", collections.map((c) => ({ ...c }))),
      create: (input) => Promise.resolve({
        id: `col-new-${collections.length}`, name: input.name, icon: input.icon ?? null,
        sortOrder: collections.length, itemCount: 0, createdAt: 0, updatedAt: 0,
      }),
      update: (id, input) => Promise.resolve({ ...collections.find((c) => c.id === id), ...input }),
      delete: () => Promise.resolve(true),
    });
    Object.assign(api.tag, {
      __parity: true,
      list: () => done("tags", tags.map((t) => ({ ...t }))),
      create: (input) => Promise.resolve({ id: `tag-new-${tags.length}`, name: input.name, colorKey: input.colorKey ?? "gray", itemCount: 0, createdAt: 0, updatedAt: 0 }),
      update: (id, input) => Promise.resolve({ ...tags.find((t) => t.id === id), ...input }),
      delete: () => Promise.resolve(true),
    });
    if (api.knowledge) Object.assign(api.knowledge, { counts: () => done("counts", JSON.parse(JSON.stringify(data.counts))) });
  };

  if (window.api) {
    patch(window.api);
    return;
  }
  let value;
  Object.defineProperty(window, "api", {
    configurable: true,
    enumerable: true,
    get: () => value,
    set: (next) => {
      value = next;
      patch(next);
    },
  });
}

/** 每个会话只登记一次初始化脚本；脚本自己看配置决定是否生效。 */
async function ensureRegistered(session) {
  if (session[REGISTERED]) return;
  await session.page.addInitScript(pageInit, { fixture: LIBRARY_FIXTURE, configKey: CONFIG_KEY });
  session[REGISTERED] = true;
}

async function reloadClean(session) {
  await session.reload();
  await session.page.addStyleTag({ content: SETTLE_CSS });
  await settle(session.page);
}

/**
 * 紧凑布局（视口 ≤ 1179.98px，useCompactLayout）下二级侧栏默认收起，要先点顶栏的「展开」让它以浮层打开
 * （浮层打开后一直留着，直到点遮罩或按 Esc）。宽屏什么也不做。
 */
export async function openCompactSidebar(win) {
  const compact = await win.evaluate(() => window.matchMedia("(max-width: 1179.98px)").matches);
  if (!compact) return;
  await win.getByRole("button", { name: "展开", exact: true }).click();
  await win.waitForTimeout(400); // 浮层的入场过渡（duration-smooth 280ms）
}

/**
 * 进入知识库侧栏：写配置与智能视图 → reload（让替换赶在渲染层启动之前）→ 点 rail「知识库」→ 等三个接口各回一次 →
 * 隐去工作区 → 紧凑布局下展开侧栏浮层。
 * options：{ variant: "data"|"empty"|"long"|"many"，smartViews: 夹具里智能视图集的名字或 null }。
 */
export async function enterLibrary(session, options = {}) {
  await ensureRegistered(session);
  const config = { variant: "data", smartViews: null, ...options };
  const views = config.smartViews ? LIBRARY_FIXTURE.smartViews[config.smartViews] : null;
  if (config.smartViews && !views) throw new Error(`夹具里没有智能视图集 ${config.smartViews}`);
  await session.page.evaluate(({ key, value, viewsKey, viewsValue }) => {
    localStorage.setItem(key, JSON.stringify(value));
    if (viewsValue) localStorage.setItem(viewsKey, JSON.stringify(viewsValue));
    else localStorage.removeItem(viewsKey);
  }, { key: CONFIG_KEY, value: config, viewsKey: SMART_VIEWS_KEY, viewsValue: views });
  await reloadClean(session);
  const { page } = session;
  await page.getByRole("button", { name: "知识库", exact: true }).click();
  await page.getByRole("button", { name: "新建合集", exact: true }).waitFor({ state: "visible" });
  await page.waitForFunction(() => {
    const seen = window.__parityLibrary;
    return !!seen && seen.collections > 0 && seen.tags > 0 && seen.counts > 0;
  });
  await page.addStyleTag({ content: HIDE_WORKSPACE_CSS });
  await openCompactSidebar(page);
  await settle(page, { quietMs: 120 });
}

/** 离开知识库：清配置与智能视图并 reload，把夹具连同页面状态一起收走。 */
export async function leaveLibrary(session) {
  await session.page.evaluate(({ key, viewsKey }) => {
    localStorage.removeItem(key);
    localStorage.removeItem(viewsKey);
  }, { key: CONFIG_KEY, viewsKey: SMART_VIEWS_KEY });
  await reloadClean(session);
}
