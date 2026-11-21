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
import { SETTLE_CSS, parityLanguage, settle } from "./react-state.mjs";

/**
 * 进入知识库与切到表格视图要点的几个控件，按参照的界面语言取可访问名
 * （renderer 的 i18n：nav.library、common.expand、library.newCollection、library.viewList）。
 * 只覆盖进入流程；各场景自己的交互步骤仍按中文界面写（英文变体只用不需要交互的静止场景，台账 G-017）。
 */
const NAV_LABELS = {
  zh: { library: "知识库", expand: "展开", newCollection: "新建合集", viewList: "列表视图" },
  en: { library: "Library", expand: "Expand", newCollection: "New collection", viewList: "List view" },
};
export const navLabel = (key) => NAV_LABELS[parityLanguage()][key];

/** 夹具：与 Viva 场景共用同一份 JSON。 */
export const LIBRARY_FIXTURE = JSON.parse(
  fs.readFileSync(path.join(import.meta.dirname, "../../../viva/parity/testdata/library-fixture.json"), "utf8"),
);

/** 详情夹具（M2-4）：列表投影里没有的正文、摘要、文字稿、来源、复核状态，来源版本，冲突与失败场景的数据；与 Viva 场景共用同一份 JSON。 */
export const LIBRARY_DETAIL_FIXTURE = JSON.parse(
  fs.readFileSync(path.join(import.meta.dirname, "../../../viva/parity/testdata/library-detail-fixture.json"), "utf8"),
);

const CONFIG_KEY = "__parity.library";
const SMART_VIEWS_KEY = "guizhi-library-smart-views-v1";
const UI_STORAGE_KEY = "ui-storage";
/** 渲染层表格视图的列配置存档（item-table-config.ts 的 STORAGE_KEY）。 */
const TABLE_CONFIG_KEY = "guizhi-library-table-config";
/**
 * 详情的阅读记忆存档（reading-memory.ts 的 STORAGE_KEY）：每个条目记着上次停在哪个标签页与滚动位置，再次打开时恢复。
 * 不清掉的话，前一个场景点过的标签（论坛的「讨论」、视频的「文字稿」、图片的「图中文字」）会带进后面的变体与场景，
 * 同一个场景在第二个变体里的默认标签就变了（2026-11-21 M2-4 的 T1 暴露）。进入与离开都清，场景之间互不影响。
 */
const READING_MEMORY_KEY = "guizhi-content-reading-v1";
const REGISTERED = Symbol("parity.library.registered");

/** 隐去工作区：侧栏面板（`aside`）紧邻的内容容器里的一切。选择器限定在「面板与内容并排」的那一层，不碰 rail 与顶栏。 */
const HIDE_WORKSPACE_CSS = `
div.relative.min-h-0.flex-1.overflow-hidden > aside + div > * { visibility: hidden !important; }
`;

/**
 * 只隐去详情栏（M2-4 之前 Viva 没有）：卡片布局里列表栏旁边那个 `min-w-0 flex-1` 的容器的内容。
 * 列表栏、工具条与分页照常显示；容器自己的底色保留，Viva 一侧对应一块只有底色的详情栏。
 * 同样是采集端的口径（页内样式），不是渲染层源码的改动。
 */
const HIDE_DETAIL_CSS = `
div.relative.min-h-0.flex-1.overflow-hidden > aside + div div.min-w-0.flex-1.app-wallpaper-section > * { visibility: hidden !important; }
`;

/**
 * 只隐去阅读区（M2-4 的详情场景用，取代 HIDE_DETAIL_CSS）：内容面板里 `[data-article-reader]` 容器的一切。
 * Viva 一侧的阅读区（Markdown 预览、楼层讨论、网页快照、图库、文字稿、编辑器）归 M2-5 与 M2-6，此前是留白（台账 B-059）；
 * 容器自己的布局（占满标签栏下面的空间）与面板底色不变，详情头部、通知条、标签栏与查找条照常显示。
 * 阅读区经门户放进工具栏挂载点（`ref={setReaderToolbar}` 那个空容器，位于工具栏右半段的最前面）的控件——「阅读版本」组（原文、AI 阅读）、
 * 目录、编辑原文 / 完成编辑与「正在编辑原文」提示——是阅读区的一部分，同样隐去（display:none，容器自己照常占一个弹性子项，相邻的 gap 照算）；
 * Viva 一侧对应一个空的挂载点（viva/screens/library/detailtoolbar.go 的 reader-toolbar）。
 * 同样是采集端的口径（页内样式），不是渲染层源码的改动；详情浮层（挂在 body 下的门户）里的阅读区同样适用。
 * 两条规则拆成两个常量：读者工具条的对照场景（config.detail.toolbar，台账 B-065）只要第一条——Viva 一侧开着 Screen.ReaderToolbar，
 * 工具条与阅读区内容的对照口径分开：阅读区内容仍隐去（Viva 的阅读岛在无头会话里画不出来），工具条照常显示。
 */
const HIDE_READER_CONTENT_CSS = `
[data-article-reader] > * { visibility: hidden !important; }
`;
const HIDE_READER_TOOLBAR_CSS = `
[data-testid="article-toolbar"] > .ml-auto > div:first-child > * { display: none !important; }
`;
const HIDE_READER_CSS = HIDE_READER_CONTENT_CSS + HIDE_READER_TOOLBAR_CSS;

/**
 * 页面初始化脚本（在页内运行，不能引用外层变量）：读配置，按配置替换 window.api 的合集、标签与计数接口。
 * 每个接口完成一次就记一笔到 window.__parityLibrary，场景据此知道侧栏已经拿到了数据（空库没有可等的文字）。
 */
function pageInit({ fixture, detail, configKey }) {
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
  const seen = (window.__parityLibrary = { collections: 0, tags: 0, counts: 0, list: 0 });
  const done = (key, value) => { seen[key] += 1; return Promise.resolve(value); };

  // 条目列表（M2-2）：规则与 Go 一侧的假实现一致（native/services/servicestest/library_list.go）——
  // 范围的含义、置顶优先、排序字段与方向、搜索取标题与摘要里的子串（结果保持全集里的先后）、游标只记下一页的起点。
  const entries = (data.entries ?? []).map((e) => ({ ...e }));
  const inScope = (e, scope) => {
    const deleted = !!e.deletedAt;
    switch (scope) {
      case "trash": return deleted;
      case "uncategorized": return !deleted && !e.collectionId && e.status !== "archived";
      case "favorites": return !deleted && e.isFavorite;
      case "archived": return !deleted && e.status === "archived";
      default: return !deleted && e.status !== "archived";
    }
  };
  const matches = (e, q) => {
    if (!inScope(e, q.scope)) return false;
    if (q.collectionId && e.collectionId !== q.collectionId) return false;
    if (q.tagId && !e.tags.some((t) => t.id === q.tagId)) return false;
    if (q.platform && e.platform !== q.platform) return false;
    const needle = (q.search ?? "").trim().toLowerCase();
    return !needle || e.title.toLowerCase().includes(needle) || e.snippet.toLowerCase().includes(needle);
  };
  const compare = (a, b, sortBy, order) => {
    if (a.isPinned !== b.isPinned) return a.isPinned ? -1 : 1;
    let cmp;
    if (sortBy === "createdAt") cmp = a.createdAt - b.createdAt;
    else if (sortBy === "title") cmp = a.title.toLowerCase() < b.title.toLowerCase() ? -1 : a.title.toLowerCase() > b.title.toLowerCase() ? 1 : 0;
    else cmp = a.updatedAt - b.updatedAt;
    if (order !== "asc") cmp = -cmp;
    if (cmp !== 0) return cmp;
    if (a.updatedAt !== b.updatedAt) return b.updatedAt - a.updatedAt;
    return a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
  };
  const listEntries = (query) => {
    const hits = entries.filter((e) => matches(e, query));
    if (!(query.search ?? "").trim()) hits.sort((a, b) => compare(a, b, query.sortBy, query.sortOrder));
    const start = typeof query.cursor === "string" && query.cursor ? Number(query.cursor.split(":")[1]) : (query.offset ?? 0);
    const limit = query.limit > 0 ? query.limit : 50;
    const end = Math.min(start + limit, hits.length);
    return {
      entries: start < hits.length ? hits.slice(start, end).map((e) => JSON.parse(JSON.stringify(e))) : [],
      total: hits.length,
      nextCursor: end < hits.length ? `c:${end}` : null,
    };
  };
  // 批量操作（M2-3）：freeze 返回筛选结果的全部 id；execute 对每个 id 执行命令并就地改条目，规则与 Go 一侧的假实现
  // （native/services/servicestest/library_ops.go 的 ExecuteBatch）一致：回收站里的条目只有恢复与彻底删除能碰，
  // 彻底删除只收回收站里的；config.batch.fail 里登记的 id 在 execute 时失败，原因取登记的文字。
  const batchFail = config.batch?.fail ?? {};
  const holdAfter = config.batch?.holdAfter ?? 0;
  let executes = 0;
  const byName = (a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0);
  const tagNamed = (name) => {
    let tag = tags.find((t) => t.name === name);
    if (!tag) {
      tag = { id: `tag-new-${tags.length + 1}`, name, colorKey: "gray", itemCount: 0, createdAt: 0, updatedAt: 0 };
      tags.push(tag);
      tags.sort(byName);
    }
    return tag;
  };
  /** 对一条执行命令；返回失败原因，成功返回空串。 */
  const applyOne = (command, id) => {
    const at = entries.findIndex((e) => e.id === id);
    if (at < 0) return "条目已删除";
    const entry = entries[at];
    const inTrash = !!entry.deletedAt;
    if (command.kind !== "restore" && command.kind !== "delete" && inTrash) return "条目已在回收站";
    if (batchFail[id]) return batchFail[id];
    switch (command.kind) {
      case "update": {
        const patch = command.patch ?? {};
        if (patch.isFavorite !== undefined) entry.isFavorite = patch.isFavorite;
        if (patch.isPinned !== undefined) entry.isPinned = patch.isPinned;
        if (patch.collectionId !== undefined) entry.collectionId = patch.collectionId;
        for (const name of patch.addTagNames ?? []) {
          if (!entry.tags.some((t) => t.name === name)) entry.tags.push({ ...tagNamed(name) });
        }
        entry.tags.sort(byName);
        entry.updatedAt = fixture.now;
        return "";
      }
      case "status":
        entry.status = command.status;
        entry.updatedAt = fixture.now;
        return "";
      case "trash":
        entry.deletedAt = fixture.now;
        entry.updatedAt = fixture.now;
        return "";
      case "restore":
        if (inTrash) {
          entry.deletedAt = 0;
          entry.updatedAt = fixture.now;
        }
        return "";
      case "delete":
        if (!inTrash) return "只能彻底删除回收站中的条目";
        entries.splice(at, 1);
        return "";
      default:
        return "批量操作类型无效";
    }
  };
  const listFault = config.list?.fault ?? "";
  // 条目详情（M2-4）：详情字段取详情夹具，叠在列表条目上；保存的正文改动写回 bodies。没有夹具条目的字段取空。
  const detailCfg = config.detail ?? null;
  const bodies = detailCfg ? JSON.parse(JSON.stringify(detail.details)) : {};
  const fullItem = (id) => {
    const e = entries.find((x) => x.id === id);
    if (!e) return null;
    const body = bodies[id] ?? {};
    return {
      ...JSON.parse(JSON.stringify(e)),
      content: body.content ?? "",
      summary: body.summary ?? "",
      transcript: body.transcript ?? "",
      sourceUri: body.sourceUri ? body.sourceUri : null,
      reviewStatus: body.reviewStatus ?? e.reviewStatus ?? "clear",
      reviewReasons: body.reviewReasons ?? e.reviewReasons ?? [],
      metadata: {},
    };
  };
  const listApi = {
    list: (query) => {
      seen.list += 1;
      if (listFault === "hold") return new Promise(() => {}); // 请求永不返回：读取中画面
      if (listFault === "fail") return Promise.reject(new Error(config.list.failMessage));
      return Promise.resolve(listEntries(query));
    },
    // 没有 config.detail 时详情栏被隐去，只要选中不报错：给一份与列表条目同源的最小详情。
    // 有 config.detail（M2-4 的详情场景）时按详情夹具给完整的详情；get 可卡住（读取中画面）或失败（错误态画面）。
    get: (id) => {
      const e = entries.find((x) => x.id === id);
      if (!detailCfg) return Promise.resolve(e ? { ...JSON.parse(JSON.stringify(e)), content: "", sourceUrl: null, metadata: {} } : null);
      if (detailCfg.get === "hold") return new Promise(() => {}); // 请求永不返回：「正在加载条目…」
      if (detailCfg.get === "fail") return Promise.reject(new Error(detail.failures.get));
      return Promise.resolve(fullItem(id));
    },
    // 草稿保存（防抖到点或 Ctrl+S）：按 config.detail.save 成功、卡住、失败或撞上冲突；成功时把补丁写进条目与详情。
    // 规则与 Go 一侧的假实现（servicestest.Library.SaveDraft）一致：回收站里的条目存不了草稿。
    saveDraft: (input) => {
      const mode = detailCfg?.save ?? "";
      if (mode === "hold") return new Promise(() => {}); // 卡在「保存中…」
      if (mode === "fail") return Promise.resolve({ ok: false, error: detail.failures.save });
      const e = entries.find((x) => x.id === input.id);
      if (!e || e.deletedAt) return Promise.resolve({ ok: false, error: "条目不存在或已移到回收站" });
      if (mode === "conflict") {
        // 另一入口已把标题改成别的：草稿没有覆盖已保存的版本，两份都留给用户选。
        return Promise.resolve({
          ok: false,
          item: { ...fullItem(input.id), title: detail.conflict.savedTitle },
          conflicts: ["title"],
          error: "该内容已在其他操作中修改，请选择要保留的版本",
        });
      }
      if (input.patch.title !== undefined) e.title = input.patch.title;
      if (input.patch.content !== undefined) (bodies[input.id] ??= {}).content = input.patch.content;
      e.updatedAt = fixture.now;
      return Promise.resolve({ ok: true, item: fullItem(input.id) });
    },
    // 批量选择：freeze 冻结筛选结果的 id（顺序与列表一致），execute 逐条执行。
    selection: (command) => {
      if (command.action === "freeze") {
        const hits = entries.filter((e) => matches(e, command.query));
        if (!(command.query.search ?? "").trim()) hits.sort((a, b) => compare(a, b, command.query.sortBy, command.query.sortOrder));
        return Promise.resolve({ ok: true, ids: hits.map((e) => e.id) });
      }
      if (command.action === "execute") {
        // holdAfter：第 N 次批量照常执行，之后的批量永不返回（批量进行中的画面，如重试失败项）。
        executes += 1;
        if (holdAfter > 0 && executes > holdAfter) return new Promise(() => {});
        const results = [...new Set(command.ids)].map((id) => {
          const error = applyOne(command.command, id);
          return error ? { id, ok: false, error } : { id, ok: true };
        });
        return Promise.resolve({ ok: true, results });
      }
      // 来源版本（详情上方的「更新的来源版本」）：config.detail.versions 为真时给详情夹具里这一条的版本，否则没有。
      if (command.action === "source-versions") {
        const versions = detailCfg?.versions ? (detail.sourceVersions[command.id] ?? []) : [];
        return Promise.resolve({ ok: true, versions: JSON.parse(JSON.stringify(versions)) });
      }
      return Promise.resolve({ ok: false, error: "不支持的选择命令" });
    },
  };

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
    if (api.knowledge) {
      Object.assign(api.knowledge, { counts: () => done("counts", JSON.parse(JSON.stringify(data.counts))) });
      if (config.list) Object.assign(api.knowledge, listApi);
    }
    // 转写能力：详情场景里固定为「不支持区分说话人」（与 Go 一侧的假实现默认值一致），不依赖真实引擎配置。
    if (detailCfg && api.media) Object.assign(api.media, { capabilities: () => Promise.resolve({ diarization: false }) });
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
  await session.page.addInitScript(pageInit, { fixture: LIBRARY_FIXTURE, detail: LIBRARY_DETAIL_FIXTURE, configKey: CONFIG_KEY });
  // 夹具的「现在」与时区：表格里条目时间的写法（今天写时分、其余写日期）取决于它们，两侧用同一组值才可比，
  // 也不会因为哪天采集而变（Playwright 的 clock.setFixedTime 只钉 Date，定时器照常跑；时区用 CDP 钉）。
  await session.page.clock.setFixedTime(new Date(LIBRARY_FIXTURE.now));
  await session.cdp.send("Emulation.setTimezoneOverride", { timezoneId: LIBRARY_FIXTURE.timezone });
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
  await win.getByRole("button", { name: navLabel("expand"), exact: true }).click();
  await win.waitForTimeout(400); // 浮层的入场过渡（duration-smooth 280ms）
}

/**
 * 进入知识库侧栏：写配置与智能视图 → reload（让替换赶在渲染层启动之前）→ 点 rail「知识库」→ 等三个接口各回一次 →
 * 隐去工作区 → 紧凑布局下展开侧栏浮层。
 * options：{ variant: "data"|"empty"|"long"|"many"，smartViews: 夹具里智能视图集的名字或 null，
 * list: 不给表示工作区整体隐去；给 { fault, failMessage } 表示列表栏是真的（M2-2），只隐去详情栏，
 * fault 是 "" | "hold"（请求永不返回）| "fail"（请求失败，原因取 failMessage）}。
 * 视图模式由场景自己点工具条上的切换钮改（渲染层把它记在 localStorage 的 ui-storage 里，leaveLibrary 复位）。
 */
export async function enterLibrary(session, options = {}) {
  await ensureRegistered(session);
  const config = { variant: "data", smartViews: null, ...options };
  const views = config.smartViews ? LIBRARY_FIXTURE.smartViews[config.smartViews] : null;
  if (config.smartViews && !views) throw new Error(`夹具里没有智能视图集 ${config.smartViews}`);
  await session.page.evaluate(({ key, value, viewsKey, viewsValue, memoryKey }) => {
    localStorage.setItem(key, JSON.stringify(value));
    if (viewsValue) localStorage.setItem(viewsKey, JSON.stringify(viewsValue));
    else localStorage.removeItem(viewsKey);
    localStorage.removeItem(memoryKey); // 上一个场景（或中途失败的场景）留下的阅读记忆
  }, { key: CONFIG_KEY, value: config, viewsKey: SMART_VIEWS_KEY, viewsValue: views, memoryKey: READING_MEMORY_KEY });
  await reloadClean(session);
  const { page } = session;
  await page.getByRole("button", { name: navLabel("library"), exact: true }).click();
  await page.getByRole("button", { name: navLabel("newCollection"), exact: true }).waitFor({ state: "visible" });
  await page.waitForFunction((withList) => {
    const seen = window.__parityLibrary;
    return !!seen && seen.collections > 0 && seen.tags > 0 && seen.counts > 0 && (!withList || seen.list > 0);
  }, !!config.list);
  // showReader：阅读岛对照工具（tools/probe-reader-island.mjs）要看阅读区本身，不隐去；其余场景不传，行为不变。
  if (!(config.detail && config.showReader)) {
    const readerCss = config.detail?.toolbar ? HIDE_READER_CONTENT_CSS : HIDE_READER_CSS; // toolbar：只隐去阅读区内容，保留读者工具条（B-065）
    await page.addStyleTag({ content: config.detail ? readerCss : config.list ? HIDE_DETAIL_CSS : HIDE_WORKSPACE_CSS });
  }
  // 侧栏场景比的就是侧栏，紧凑布局下先把浮层展开；列表场景比的是工作区里的列表栏，浮层会盖住它，
  // 需要点侧栏行的场景自己用 openCompactSidebar / closeCompactSidebar 开关。
  if (!config.list) await openCompactSidebar(page);
  await settle(page, { quietMs: 120 });
}

/** 紧凑布局下关掉侧栏浮层（Esc）；宽屏什么也不做。 */
export async function closeCompactSidebar(win) {
  const compact = await win.evaluate(() => window.matchMedia("(max-width: 1179.98px)").matches);
  if (!compact) return;
  await win.keyboard.press("Escape");
  await win.waitForTimeout(400); // 浮层的退场过渡
}

/**
 * 离开知识库：清配置与智能视图、把列表的视图模式与栏宽复位，并 reload，把夹具连同页面状态一起收走。
 * 视图模式与列表栏宽度存在渲染层 ui store 的持久化里（ui-storage），不复位会带进后面的场景。
 */
export async function leaveLibrary(session) {
  await session.page.evaluate(({ key, viewsKey, uiKey, tableKey, memoryKey }) => {
    localStorage.removeItem(key);
    localStorage.removeItem(tableKey); // 表格视图的列配置（显示与列宽）也在 localStorage 里
    localStorage.removeItem(viewsKey);
    localStorage.removeItem(memoryKey); // 详情的阅读记忆（上次停在哪个标签页）
    try {
      const saved = JSON.parse(localStorage.getItem(uiKey) || "null");
      if (saved?.state) {
        saved.state.libraryViewMode = "card";
        delete saved.state.itemListPaneWidth;
        localStorage.setItem(uiKey, JSON.stringify(saved));
      }
    } catch {
      localStorage.removeItem(uiKey); // 坏掉的存档直接丢，渲染层会用缺省值
    }
  }, { key: CONFIG_KEY, viewsKey: SMART_VIEWS_KEY, uiKey: UI_STORAGE_KEY, tableKey: TABLE_CONFIG_KEY, memoryKey: READING_MEMORY_KEY });
  await reloadClean(session);
}
