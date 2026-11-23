/**
 * React 侧场景注册表：每个场景知道怎样从任意状态走到自己、取哪个区域。
 *
 * 场景 ID 与盘点清单（docs/agents/viva-parity-inventory/）一致：`<模块>.<屏幕>[.<状态>]`。
 * `enter(ctx)` 必须是幂等的——从任意界面状态出发都能走到该场景（同一会话里连续采集多个场景时，上一个场景留下的状态不能影响下一个）。
 * `region`：阶段 1 校准用的区域场景（取该元素的外框，Viva 侧在同尺寸画布里只画这块）；缺省是整个视口。
 *
 * 渲染层只允许加 data-testid / aria 属性（见台账「渲染层改动登记」）；能用现有角色与名称定位的，不去改渲染层。
 * `leave(ctx)`：可选，场景采集完后调用，把场景留下的东西收掉（提示场景用它收走提示、恢复页面）。
 * `keepPointer`：可选，悬停态场景（行悬停）为真：采集器不挪走指针、不清焦点，也不校验悬停残留；默认一律挪走并校验。
 * `keepFocus`：可选，为真时采集器照常挪走指针并校验没有悬停残留，但不清焦点（菜单打开后第一项自动聚焦，那是用户看到的自然状态）。
 */
import fs from "node:fs";
import path from "node:path";
import { IMPORTS_FIXTURE, enterImports, leaveImports } from "../lib/react-imports.mjs";
import { resetToasts, showToasts } from "../lib/react-toast.mjs";
import { LIBRARY_SCENES } from "./react-library-scenes.mjs";
import { LIBRARY_LIST_SCENES } from "./react-library-list-scenes.mjs";
import { LIBRARY_TABLE_SCENES } from "./react-library-table-scenes.mjs";
import { LIBRARY_BULK_SCENES } from "./react-library-bulk-scenes.mjs";
import { LIBRARY_DETAIL_SCENES } from "./react-library-detail-scenes.mjs";
import { INBOX_SCENES } from "./react-inbox-scenes.mjs";
import { SETTINGS_CORE_SCENES } from "./react-settings-core-scenes.mjs";
import { SETTINGS_ADVANCED_SCENES } from "./react-settings-advanced-scenes.mjs";
import { CAPTURE_SCENES } from "./react-capture-scenes.mjs";
import { ASK_SCENES } from "./react-ask-scenes.mjs";
import { THEMED_SCENES } from "./react-themed-scenes.mjs";
import { WIKI_SCENES } from "./react-wiki-scenes.mjs";
import { SETTINGS_CONTENT_SCROLLER, openSettingsSection, scrollSettingsToEnd } from "../lib/react-settings-nav.mjs";

/**
 * 把设置内容滚到「locator 的上沿离内容区上沿 offset 像素」（到末端就停）。Viva 侧（scrollToControl）按同样的距离滚，
 * 两边落在同一个滚动位置：目标不在可见区里时 Viva 看不到它，没法用「滚到底」让目标在所有视口里都看得见（800x600 里它在底部之上很远）。
 */
async function scrollSettingsTo(win, locator, offset = 120) {
  const area = win.locator(SETTINGS_CONTENT_SCROLLER);
  const [a, b] = await Promise.all([area.boundingBox(), locator.boundingBox()]);
  await area.evaluate((el, dy) => {
    el.scrollTop += dy;
  }, b.y - a.y - offset);
}

/** 打开「显示设置」并点色带里的「自定义」：面板展开后把指针挪到左上角。主题色按自定义的十六进制重新取色。 */
async function openCustomColor(win) {
  await openSettingsSection(win, "appearance");
  await win.getByRole("button", { name: "自定义", exact: true }).first().click();
  await win.getByPlaceholder("#3b82f6", { exact: true }).waitFor({ state: "visible" });
  await win.mouse.move(2, 2);
}

/**
 * 打开「显示设置」，选「自定义」展开面板，再点面板里的取色器触发钮：弹层（role=dialog，portal 到 body 的 fixed 面板）出现后
 * 把指针挪到左上角。触发钮与色带里的预设同名，是第二个名为「自定义」的按钮。
 */
async function openCustomPicker(win) {
  await openCustomColor(win);
  await win.getByRole("button", { name: "自定义", exact: true }).nth(1).click();
  await win.getByRole("dialog", { name: "自定义", exact: true }).waitFor({ state: "visible" });
  await win.mouse.move(2, 2);
}

/**
 * 打开设置页「应用设置」里的下拉：先收起可能残留的浮层，点触发器，等 listbox 出现，再把指针挪到左上角——
 * 触发器与选项的悬停态不能混进画面（Viva 侧的无头会话没有指针）。
 * name 是触发器的可访问名（aria-label，简体中文界面）。
 * scrollEnd 为真时先把设置内容滚到底：语言下拉在窗口底部，不滚就被视口边缘截住（Viva 侧同样先滚到底）。
 */
async function openSettingsSelect(win, name, { scrollEnd = false } = {}) {
  await openSettingsSection(win, "general");
  await win.keyboard.press("Escape");
  if (scrollEnd) {
    await win.locator(SETTINGS_CONTENT_SCROLLER).evaluate((el) => {
      el.scrollTop = el.scrollHeight;
    });
  }
  await win.getByRole("button", { name, exact: true }).click();
  await win.getByRole("listbox", { name, exact: true }).waitFor({ state: "visible" });
  await win.mouse.move(2, 2);
}

/**
 * 打开设置页「应用设置」里的「后台任务」确认对话框：把开关从关打开，等对话框出现。
 * 对话框不确认也不取消：场景停在它打开的状态；下一个场景的 openSettingsSection 先按 Esc 把它关掉。
 */
async function openBackgroundTasksDialog(win) {
  await openSettingsSection(win, "general");
  await win.getByRole("switch", { name: "后台任务", exact: true }).click();
  await win.getByRole("alertdialog").waitFor({ state: "visible" });
}

/**
 * 打开顶栏「新建」旁的创建方式菜单（台账 B-007）：点箭头按钮，等菜单出现，再把指针挪到左上角。
 * 菜单打开时第一项自动聚焦（渲染层 ContextMenu 的 useEffect），场景用 keepFocus 保留这个状态。
 */
async function openNewMenu(win) {
  await openSettingsSection(win, "general");
  await win.keyboard.press("Escape");
  await win.getByRole("button", { name: "创建方式", exact: true }).click();
  await win.getByRole("menu").waitFor({ state: "visible" });
  await win.mouse.move(2, 2);
}

/** 菜单打开后按一次 ↓：焦点从第一项挪到第二项，键盘模态下 :focus-visible 成立，出现全局的聚焦环。 */
async function openNewMenuKeyboard(win) {
  await openNewMenu(win);
  await win.keyboard.press("ArrowDown");
  await win.mouse.move(2, 2);
}

/**
 * 打开「关闭应用」对话框（CloseDialog，Windows 点关闭钮且设置为「每次询问」时由主进程推 window:showCloseDialog）。
 * 采集器没有真实的主进程，直接走 Go 推送到页面的同一条路径：MyGo 运行时的 `__mygo.receive` 收到 "ipc" 事件，
 * shim 再按频道分发给渲染层注册的监听器——驱动的是渲染层自己的 App 状态与 CloseDialog，不改渲染层一行代码。
 */
async function openCloseDialog(win) {
  await openSettingsSection(win, "general");
  await win.keyboard.press("Escape");
  await win.evaluate(() => {
    window.__mygo.receive({ t: "event", n: "ipc", p: { channel: "window:showCloseDialog", args: [] } });
  });
  await win.getByRole("alertdialog").waitFor({ state: "visible" });
}

/** 「有新版本」场景里推送的版本号（Viva 场景用同一个值）。 */
export const UPDATE_VERSION = "1.2.3";

/**
 * 顶栏出现「有新版本」按钮（B-006）：主进程推 updater:status = available 时，App 把状态存下来，
 * 顶栏在新建按钮之前多出虚线边框的更新按钮与一条竖分隔线。
 * 走与 openCloseDialog 同一条路径（Go 推给页面的 ipc 事件），驱动渲染层自己的 App 状态，不改渲染层一行代码。
 */
async function showUpdateButton(win) {
  await openSettingsSection(win, "general");
  await win.evaluate((version) => {
    window.__mygo.receive({ t: "event", n: "ipc", p: { channel: "updater:status", args: [{ status: "available", info: { version } }] } });
  }, UPDATE_VERSION);
  const button = win.getByRole("button", { name: "发现新版本", exact: true });
  await button.waitFor({ state: "visible" });
  // 自动检查更新的回包（not-available）会把状态清掉：等一小会儿再确认按钮还在，免得采到一半消失。
  await win.waitForTimeout(400);
  await button.waitFor({ state: "visible", timeout: 1000 });
}

/** 关闭对话框里点一下「记住我的选择」的文字：label 的 onClick 翻转，不取键盘焦点。 */
async function openCloseDialogChecked(win) {
  await openCloseDialog(win);
  await win.getByText("记住我的选择", { exact: true }).click();
  await win.mouse.move(2, 2);
}

/**
 * 把指针停在关闭对话框里名为 name 的选项按钮中心（悬停态：场景用 keepPointer 保留）。
 * 采集器的 keepPointer 路径不清焦点，而对话框容器是程序聚焦的（dialog.focus()），Chromium 会给它画默认的 :focus-visible 外框
 * （黑色细线，视觉上是对话框外圈）；其余场景的采集器都会 blur 掉它，所以这里照样 blur：悬停保持，焦点清掉（台账 G-007）。
 */
function hoverCloseOption(name) {
  return async (win) => {
    await openCloseDialog(win);
    await win.getByRole("button", { name, exact: true }).hover();
    await win.evaluate(() => { if (document.activeElement instanceof HTMLElement) document.activeElement.blur(); });
  };
}

/** 关闭对话框打开后按一次 Tab：焦点从对话框容器落到页眉关闭钮，键盘模态下出聚焦环（场景用 keepFocus 保留）。 */
async function openCloseDialogKeyboard(win) {
  await openCloseDialog(win);
  await win.keyboard.press("Tab");
  await win.mouse.move(2, 2);
}

/** 提示场景的文案：与 Viva 场景共用同一份 JSON（viva/parity/testdata/toast-content.json）。 */
const TOAST_TEXT = JSON.parse(
  fs.readFileSync(path.join(import.meta.dirname, "../../../viva/parity/testdata/toast-content.json"), "utf8"),
);

/** 提示场景：弹出一组提示（页面内容隐去，只留提示），可选再展开第一条的详情；离开时收走并恢复页面。 */
function toastScene(specs, { expandDetail = false } = {}) {
  return {
    module: "ui",
    async enter({ win }) {
      await showToasts(win, specs);
      if (expandDetail) {
        await win.getByRole("button", { name: "查看详情", exact: true }).first().click();
        await win.getByRole("button", { name: "复制", exact: true }).waitFor({ state: "visible" });
        await win.mouse.move(2, 2);
      }
    },
    async leave({ win }) {
      await resetToasts(win);
    },
  };
}

/**
 * 采集队列场景：进入时装夹具并 reload，可选再做一段交互（steps）；离开时清夹具并 reload。
 * 整窗对照，不给 region。
 */
function importsScene(options, steps, { keepPointer = false } = {}) {
  return {
    module: "imports",
    keepPointer,
    async enter({ win, session }) {
      await enterImports(session, options);
      if (steps) await steps(win, session);
    },
    async leave({ session }) {
      await leaveImports(session);
    },
  };
}

// 采集页的操作步骤。每一步都以「指针停在窗口左上角（没有悬停）」收尾（行悬停场景除外），
// Viva 一侧（viva/parity/scenes/imports.go）做同样的操作，最终画面才可比。

/** 搜索框填成没有匹配的词，列表按它筛选后让输入框失焦：画面上是词在框里、没有焦点环与光标。 */
async function searchNoMatch(win) {
  const box = win.getByPlaceholder("搜索标题或链接");
  await box.fill(IMPORTS_FIXTURE.noMatchQuery);
  await win.getByText("当前筛选下没有任务", { exact: true }).waitFor({ state: "visible" });
  await box.evaluate((el) => el.blur());
  await win.mouse.move(2, 2);
}

/** 依次点第 indexes 行的选择钮（行首的类型图标）。第一次选择后批量条出现、列表下移，locator 每次重新定位。 */
function selectImportRows(...indexes) {
  return async (win) => {
    const boxes = win.getByRole("button", { name: "选择", exact: true });
    for (const i of indexes) await boxes.nth(i).click();
    await win.mouse.move(2, 2);
  };
}

/**
 * 紧凑布局（视口 ≤ 1179.98px，useCompactLayout）下二级侧栏默认收起，里面的按钮被 rail 盖住点不到：
 * 先点顶栏的「展开」让它以浮层打开（浮层打开后一直留着，直到点遮罩或按 Esc，筛选之后也不自动收起）。
 */
async function openCompactSidebar(win) {
  const compact = await win.evaluate(() => window.matchMedia("(max-width: 1179.98px)").matches);
  if (!compact) return;
  await win.getByRole("button", { name: "展开", exact: true }).click();
  await win.waitForTimeout(400); // 浮层的入场过渡（duration-smooth 280ms）
}

/** 点一个按钮（侧栏的状态筛选、来源筛选等），等列表按新筛选重取一轮。 */
function clickImportsButton(name, { group } = {}) {
  return async (win) => {
    // 来源筛选在工作区工具条里（带 group），不在二级侧栏；只有侧栏里的按钮才需要先展开浮层——浮层还会盖住工作区。
    if (!group) await openCompactSidebar(win);
    const scope = group ? win.getByRole("group", { name: group, exact: true }) : win;
    await scope.getByRole("button", { name, exact: true }).click();
    await win.waitForTimeout(300);
    await win.mouse.move(2, 2);
  };
}

/** 把任务列表滚到底（「加载更早的任务」钮所在的滚动容器）。 */
async function scrollImportsToEnd(win) {
  await win.getByRole("button", { name: "加载更早的任务", exact: true }).evaluate((el) => {
    for (let p = el.parentElement; p; p = p.parentElement) {
      if (p.scrollHeight > p.clientHeight && /(auto|scroll)/.test(getComputedStyle(p).overflowY)) {
        p.scrollTop = p.scrollHeight;
        return;
      }
    }
  });
  await win.mouse.move(2, 2);
}

/**
 * 把指针停在标题为 title 的那一行的左内边距处（距行左上角 6px、20px）：行进入悬停态（底色、前导勾选框、右上的动作条），
 * 但指针不落在任何带 title 提示的元素上——气泡（TooltipLayer，台账 B-011）是另一个场景要对照的状态。
 */
function hoverImportsRow(title) {
  return async (win) => {
    const row = win
      .getByRole("button", { name: title, exact: true })
      .first()
      .locator("xpath=ancestor::*[contains(@class,'group/row')][1]");
    const box = await row.boundingBox();
    await win.mouse.move(box.x + 6, box.y + 20);
  };
}

/**
 * 悬停提示（TooltipLayer，台账 B-011）：把指针停在 pick 选中的元素中心，等 320ms 延迟后气泡出现并走完入场过渡。
 * pick(win) 返回宿主的 locator；采集器的 keepPointer 保持指针不动（移开会收起气泡）。
 */
function hoverTooltip(pick) {
  return async (win) => {
    const host = await pick(win);
    const box = await host.boundingBox();
    const x = box.x + box.width / 2;
    const y = box.y + box.height / 2;
    await win.mouse.move(x, y);
    try {
      await win.locator('[role="tooltip"]').waitFor({ state: "visible", timeout: 5000 });
    } catch (error) {
      // 没出气泡多半是指针没落在宿主上：把落点与宿主的现状写进错误，免得再盲猜
      const info = await win.evaluate(([px, py]) => {
        const el = document.elementFromPoint(px, py);
        return { tag: el?.tagName, cls: String(el?.className ?? "").slice(0, 80), title: el?.getAttribute("title"), innerHeight: window.innerHeight };
      }, [x, y]);
      throw new Error(`悬停提示没有出现：指针 (${x}, ${y})，宿主外框 ${JSON.stringify(box)}，落点 ${JSON.stringify(info)}；${error.message.split("\n")[0]}`);
    }
    await win.waitForTimeout(400); // 入场过渡（duration-quick）走完
  };
}

/** 顶栏的侧栏开关：宽屏是「收起」，紧凑布局是「展开」；两个文案都带 title。 */
const topBarToggle = (win) => win.locator('button[title="收起"], button[title="展开"]').first();

/** 第一个可打开成果的任务标题（标题按钮，title 是原始地址；行是已完成的「本地优先软件的七个理想」）。 */
const openableTitle = (win) => win.getByRole("button", { name: "本地优先软件的七个理想", exact: true }).first();

/**
 * 把指针停在名字匹配 name 的第一个按钮的中心，等颜色过渡（transition-colors 150ms）走完。
 * 用于没有 title 提示的按钮（耗时摘要）：悬停后只有颜色与下划线变化。
 */
function hoverImportsButton(name) {
  return async (win) => {
    const box = await win.getByRole("button", { name }).first().boundingBox();
    await win.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
    await win.waitForTimeout(400);
  };
}

/**
 * 筛选到「失败」后列表里的第一个带 title 的任务标题（显示名顶掉了原始地址）：失败任务里排第一的是知乎那条，
 * 原始地址 57 个字符，比气泡的 max-w-xs 长，折成两行；它在列表最上方，下方放得下，气泡落在它下面。
 * 任务行的时间列也有长提示（「入队 …｜更新 …」），但行一悬停，左右两个动作钮（含删除）就把它整个盖住（钮间缝隙 1.75px、
 * 两端各不到 1px），鼠标几乎碰不到，所以不拿它当宿主。
 */
const firstNamedRowTitle = (win) => win.locator('[class*="group/row"] :is(p, button).truncate[title]').first();

/** 点筛选「失败 N」之后悬停在 pick 选中的元素上；两步合成一个场景步骤。 */
function filterThenHoverTooltip(filterName, pick) {
  const filter = clickImportsButton(filterName);
  const hover = hoverTooltip(pick);
  return async (win) => {
    await filter(win);
    await hover(win);
  };
}

/**
 * 视口里完整可见、且指针真的落得到的最靠下的一个任务标题（带 title 的那种：显示名顶掉了原始地址）：
 * 它下方放不下气泡时，提示翻到它上方。取标题而不是时间列，因为行一悬停右上角的动作条就盖在时间列上，指针落不到它；
 * 标题在行的左侧，动作条够不着。
 * 「落得到」指中心点的最上层元素是它自己或它的后代——被滚动容器裁掉的那半截行在视口坐标里看得见，指针却落在别处。
 */
async function lowestVisibleRowName(win) {
  const labels = win.locator('[class*="group/row"] :is(p, button).truncate[title]');
  const count = await labels.count();
  const innerHeight = await win.evaluate(() => window.innerHeight);
  let best = null;
  let bestBottom = -Infinity;
  for (let i = 0; i < count; i++) {
    const box = await labels.nth(i).boundingBox();
    if (!box || box.y + box.height > innerHeight - 2 || box.y + box.height <= bestBottom) continue;
    const reachable = await labels.nth(i).evaluate((el) => {
      const r = el.getBoundingClientRect();
      const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
      return !!hit && el.contains(hit);
    });
    if (!reachable) continue;
    best = labels.nth(i);
    bestBottom = box.y + box.height;
  }
  if (!best) throw new Error("视口里没有完整可见、指针落得到的任务标题");
  return best;
}

/**
 * 打开任务详情弹窗（B-014）：在列表里找标题（以及状态徽标文字）匹配的那一行，直接对它的「任务详情」钮派发 click。
 * 行内动作条默认是 pointer-events-none（行悬停才可点），派发事件不依赖悬停；弹窗出现后把指针挪到左上角。
 */
function openImportDetail(title, { status } = {}) {
  return async (win) => {
    let row = win.locator('[class*="group/row"]').filter({ hasText: title });
    if (status) row = row.filter({ hasText: status });
    await row.first().getByRole("button", { name: "任务详情", exact: true }).dispatchEvent("click");
    await win.getByRole("dialog").waitFor({ state: "visible" });
    await win.mouse.move(2, 2);
  };
}

/** 夹具里各任务的标题（与 Viva 场景用同一份数据）。 */
const importTitle = (id) => IMPORTS_FIXTURE.tasks.find((t) => t.id === id).displayName;

/** 点「清理当前筛选」，等确认框出现。 */
async function openClearConfirm(win) {
  await win.getByRole("button", { name: "清理当前筛选", exact: true }).click();
  await win.getByRole("alertdialog").waitFor({ state: "visible" });
  await win.mouse.move(2, 2);
}

/** 设置页外壳（侧栏 + 内容）：SettingsPage 的根元素，即内容滚动容器的父元素。 */
const SETTINGS_SHELL = 'div:has(> div > [data-testid="settings-content-shell"])';

// ---- 控件状态场景（台账 B-004）：悬停、键盘聚焦 ----
// 采集器默认把指针挪走、把焦点清掉；这些场景带 keepPointer / keepFocus 保留自己要的状态。
// 过渡在采集时被 SETTLE_CSS 压成瞬时（见 collect-react.mjs），拿到的是终态；过渡本身由 Go 的 L4 测试按渲染层读数核对。

/** 应用设置页：把指针停在「后台任务」那一行的左上内边距处（SettingItem 的 hover:bg-muted/20，不碰行内的任何控件）。 */
async function hoverBackgroundTasksRow(win) {
  await openSettingsSection(win, "general");
  const row = win.getByText("后台任务", { exact: true }).first().locator("xpath=ancestor::div[contains(@class,'justify-between')][1]");
  const box = await row.boundingBox();
  await win.mouse.move(box.x + 8, box.y + 6);
}

/**
 * 键盘聚焦：先按一次 Tab 让页面进入键盘模态，再程序聚焦 locate(win) 取到的控件（此时 :focus-visible 成立），最后把指针挪走。
 * scrollTo 为真时聚焦之后再把控件滚到离内容区上沿 120px 处（Viva 侧同样先走 Tab 聚焦、再按位置滚轮：焦点留在原控件上，滚动位置才与这里一致）。
 */
function focusByKeyboard(section, locate, { scrollTo = false } = {}) {
  return async (win) => {
    await openSettingsSection(win, section);
    await win.keyboard.press("Tab");
    await locate(win).focus();
    if (scrollTo) await scrollSettingsTo(win, locate(win));
    await win.mouse.move(2, 2);
  };
}

/** 把指针停在 locator 的中心（悬停态）。 */
async function hoverCenter(win, locator) {
  const box = await locator.boundingBox();
  await win.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
}

/** 应用设置页的下拉触发器（可访问名是 aria-label）。 */
const closeActionSelect = (win) => win.getByRole("button", { name: "关闭窗口行为", exact: true });
/** 显示设置页背景图片区的主按钮（没有背景图时叫「选择图片」）。 */
const pickImageButton = (win) => win.getByRole("button", { name: "选择图片", exact: true });
/** 自定义主题色面板里的取色器触发钮：与色带里的预设同名，是第二个名为「自定义」的按钮。 */
const pickerTrigger = (win) => win.getByRole("button", { name: "自定义", exact: true }).nth(1);

/** 显示设置页：把指针停在 locate(win) 取到的控件中心（悬停态）。 */
function hoverInAppearance(locate) {
  return async (win) => {
    await openSettingsSection(win, "appearance");
    const box = await locate(win).boundingBox();
    await win.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  };
}

/** 主题模式分段里没被选中的「跟随系统」（变体的主题模式永远是浅色或深色，它从不选中）；页面里另有一枚同名的动效偏好按钮，取第一枚。 */
const unselectedSegment = (win) => win.getByRole("button", { name: "跟随系统", exact: true }).first();
/** 内容区里第一枚被选中的按钮：主题模式分段里选中的那一枚（在色带与字号之上）。限定在内容滚动区里：侧栏的当前分区项同样带 aria-pressed，在 DOM 里还更靠前。 */
const selectedSegment = (win) => win.locator(SETTINGS_CONTENT_SCROLLER).locator('button[aria-pressed="true"]').first();
/** 色带里选中的那枚预设色板（40×40 的圆形按钮）。 */
const selectedSwatch = (win) => win.locator(SETTINGS_CONTENT_SCROLLER).locator('button.rounded-full.h-10.w-10[aria-pressed="true"]').first();
/** 色带最后的「自定义」入口：没选自定义时它从不选中。 */
const customSwatch = (win) => win.getByRole("button", { name: "自定义", exact: true }).first();

export const SCENES = {
  // 知识库侧栏（M2-1）：场景在 react-library-scenes.mjs
  ...LIBRARY_SCENES,
  ...LIBRARY_LIST_SCENES,
  ...LIBRARY_TABLE_SCENES,
  ...LIBRARY_BULK_SCENES,
  ...LIBRARY_DETAIL_SCENES,
  // 处理中心（M3-1）：场景在 react-inbox-scenes.mjs
  ...INBOX_SCENES,
  // 设置页四个核心分区（M3-2a）：场景在 react-settings-core-scenes.mjs
  ...SETTINGS_CORE_SCENES,
  // 设置页三个外围分区（M3-2b）：采集与转写、MCP、AI 模型服务；场景在 react-settings-advanced-scenes.mjs
  ...SETTINGS_ADVANCED_SCENES,
  // 快速采集对话框（M3-3）：空草稿、链接、分享口令、文件加标签加合集；场景在 react-capture-scenes.mjs
  ...CAPTURE_SCENES,
  // AI 问答（M3-4）：全局问答四态与知识库详情的提问栏；场景在 react-ask-scenes.mjs
  ...ASK_SCENES,
  // Wiki（M3-5a）：空态、页面详情、编辑态、更新建议、编译对话框；场景在 react-wiki-scenes.mjs（关系图谱不纳入像素对拍）
  ...WIKI_SCENES,
  ...THEMED_SCENES,
  "settings.shell": {
    module: "settings",
    region: SETTINGS_SHELL,
    async enter({ win }) {
      await openSettingsSection(win, "general");
    },
  },
  // 整窗（顶栏 + rail + 设置页）；不给 region，取整个视口。
  "app.window.page-settings": {
    module: "app",
    async enter({ win }) {
      await openSettingsSection(win, "general");
    },
  },
  // 整窗 + 下拉面板展开（面板是 portal 到 body 的 fixed 元素，只能在整窗里对照）。
  "app.window.page-settings.select-close-action": {
    module: "app",
    async enter({ win }) {
      await openSettingsSelect(win, "关闭窗口行为");
    },
  },
  "app.window.page-settings.select-language": {
    module: "app",
    async enter({ win }) {
      await openSettingsSelect(win, "语言", { scrollEnd: true });
    },
  },
  // 整窗 + 「后台任务」确认对话框（模态：遮罩盖住整窗，面板居中）。
  "app.window.page-settings.dlg-background-tasks": {
    module: "app",
    async enter({ win }) {
      await openBackgroundTasksDialog(win);
    },
  },
  // 整窗 + 「关闭应用」对话框（模态：遮罩盖住整窗，面板居中；打开时对话框容器自己取焦点，采集前清掉）。
  "app.window.page-settings.dlg-close": {
    module: "app",
    async enter({ win }) {
      await openCloseDialog(win);
    },
  },
  // 同上，勾上「记住我的选择」；悬停两枚选项（边框与图标块随悬停变色）；Tab 一次（页眉关闭钮带聚焦环）。
  "app.window.page-settings.dlg-close-checked": {
    module: "app",
    async enter({ win }) {
      await openCloseDialogChecked(win);
    },
  },
  "app.window.page-settings.dlg-close-hover-minimize": {
    module: "app",
    keepPointer: true,
    async enter({ win }) {
      await hoverCloseOption("最小化到托盘")(win);
    },
  },
  "app.window.page-settings.dlg-close-hover-exit": {
    module: "app",
    keepPointer: true,
    async enter({ win }) {
      await hoverCloseOption("退出应用")(win);
    },
  },
  "app.window.page-settings.dlg-close-key": {
    module: "app",
    keepFocus: true,
    async enter({ win }) {
      await openCloseDialogKeyboard(win);
    },
  },
  // 整窗 + 顶栏「新建」旁的创建方式菜单（portal 到 body 的 fixed 菜单；第一项自动聚焦）。
  "app.window.page-settings.new-menu": {
    module: "app",
    keepFocus: true,
    async enter({ win }) {
      await openNewMenu(win);
    },
  },
  // 整窗 + 顶栏的「有新版本」按钮（虚线边框 border-primary/50、bg-primary/10）与右侧竖分隔线。
  "app.window.page-settings.update-available": {
    module: "app",
    async enter({ win }) {
      await showUpdateButton(win);
    },
  },
  // 同上，再按一次 ↓：第二项带键盘聚焦环。
  "app.window.page-settings.new-menu-key": {
    module: "app",
    keepFocus: true,
    async enter({ win }) {
      await openNewMenuKeyboard(win);
    },
  },
  // 提示（Toast）：整窗，页面内容隐去，只留右下角的提示（见 lib/react-toast.mjs）。
  "ui.toast.success": toastScene([{ message: TOAST_TEXT.success, type: "success" }]),
  "ui.toast.info": toastScene([{ message: TOAST_TEXT.info, type: "info" }]),
  "ui.toast.warning": toastScene([{ message: TOAST_TEXT.warning, type: "warning" }]),
  "ui.toast.error": toastScene([{ message: TOAST_TEXT.error, type: "error" }]),
  "ui.toast.error-detail": toastScene([{ message: TOAST_TEXT.error, type: "error", detail: TOAST_TEXT.detail }]),
  "ui.toast.detail-expanded": toastScene([{ message: TOAST_TEXT.error, type: "error", detail: TOAST_TEXT.detail }], { expandDetail: true }),
  "ui.toast.action": toastScene([{ message: TOAST_TEXT.undo, undo: true }]),
  // 三条叠在一起：带详情的错误、成功、撤销（gap-3 堆叠、同宽、最新的在最下面）。
  "ui.toast.container": toastScene([
    { message: TOAST_TEXT.error, type: "error", detail: TOAST_TEXT.detail },
    { message: TOAST_TEXT.success, type: "success" },
    { message: TOAST_TEXT.undo, undo: true },
  ]),
  // 采集队列整窗（顶栏 + rail + 二级侧栏 + 任务列表）：夹具与时间见 lib/react-imports.mjs。
  "app.window.imports": importsScene({}),
  "app.window.imports.loading": importsScene({ mode: "loading" }),
  "app.window.imports.error": importsScene({ mode: "error", errorMessage: IMPORTS_FIXTURE.loadError }),
  "app.window.imports.empty": importsScene({ mode: "empty" }),
  "app.window.imports.filtered-empty": importsScene({}, searchNoMatch),
  "app.window.imports.selection": importsScene({}, selectImportRows(1, 3)),
  "app.window.imports.filter-failed": importsScene({}, clickImportsButton("失败 3")),
  "app.window.imports.origin-mobile": importsScene({}, clickImportsButton("手机端", { group: "提交来源" })),
  "app.window.imports.paused": importsScene({ paused: true }),
  "app.window.imports.load-more": importsScene({ hasMore: true }, scrollImportsToEnd),
  "app.window.imports.row-hover": importsScene({}, hoverImportsRow("本地优先软件的七个理想"), { keepPointer: true }),
  "app.window.imports.clear-confirm": importsScene({}, openClearConfirm),
  // 任务详情弹窗（台账 B-014）：整窗 + 模态。各状态取夹具里对应的任务；夹具里没有的状态（刷新来源待确认、手机端的接收时间）
  // 用 overrides 在 completed / video-done 上补字段。
  "app.window.imports.dlg-detail-completed": importsScene({}, openImportDetail(importTitle("task-completed"), { status: "已完成" })),
  "app.window.imports.dlg-detail-video": importsScene({}, openImportDetail(importTitle("task-video-done"))),
  "app.window.imports.dlg-detail-warning": importsScene({}, openImportDetail(importTitle("task-warning"))),
  "app.window.imports.dlg-detail-failed": importsScene({}, openImportDetail(importTitle("task-failed"))),
  "app.window.imports.dlg-detail-processing": importsScene({}, openImportDetail(importTitle("task-processing"))),
  "app.window.imports.dlg-detail-duplicate": importsScene({}, openImportDetail(importTitle("task-duplicate"), { status: "重复内容" })),
  "app.window.imports.dlg-detail-canceled": importsScene({}, openImportDetail(importTitle("task-canceled"))),
  "app.window.imports.dlg-detail-long": importsScene({}, openImportDetail(importTitle("task-long"))),
  "app.window.imports.dlg-detail-auth": importsScene({}, openImportDetail(importTitle("task-auth"))),
  "app.window.imports.dlg-detail-refresh": importsScene(
    { overrides: { "task-completed": { refreshOfItemId: "item-005" } } },
    openImportDetail(importTitle("task-completed"), { status: "已完成" }),
  ),
  "app.window.imports.dlg-detail-mobile": importsScene(
    { overrides: { "task-video-done": { receivedAt: 1794790795000 } } },
    openImportDetail(importTitle("task-video-done")),
  ),
  // 悬停提示（TooltipLayer）：整窗 + 气泡。宿主取自采集队列页——顶栏图标钮（单行短提示）、筛选到失败后第一条的任务标题
  // （长地址折成两行，气泡在下方）、最靠下的任务标题（翻到上方）。
  "ui.tooltip.below": importsScene({}, hoverTooltip(topBarToggle), { keepPointer: true }),
  "ui.tooltip.wrap": importsScene({}, filterThenHoverTooltip("失败 3", firstNamedRowTitle), { keepPointer: true }),
  "ui.tooltip.flip": importsScene({}, hoverTooltip(lowestVisibleRowName), { keepPointer: true }),
  // 悬停下划线（台账 B-012）：可打开成果的任务标题（hover:text-primary hover:underline，同时出标题的提示气泡），
  // 与行上的耗时摘要（hover:text-foreground hover:underline，11px，没有提示）。
  "app.window.imports.title-hover": importsScene({}, hoverTooltip(openableTitle), { keepPointer: true }),
  "app.window.imports.summary-hover": importsScene({}, hoverImportsButton(/^共 \d/), { keepPointer: true }),
  "settings.general": {
    module: "settings",
    region: SETTINGS_CONTENT_SCROLLER,
    async enter({ win }) {
      await openSettingsSection(win, "general");
    },
  },
  "settings.appearance": {
    module: "settings",
    region: SETTINGS_CONTENT_SCROLLER,
    async enter({ win }) {
      await openSettingsSection(win, "appearance");
    },
  },
  // 显示设置滚到内容底（背景图分区）：与应用设置的语言下拉同样先把设置内容滚到底。
  "settings.appearance.bottom": {
    module: "settings",
    region: SETTINGS_CONTENT_SCROLLER,
    async enter({ win }) {
      await openSettingsSection(win, "appearance");
      await scrollSettingsToEnd(win);
    },
  },
  // 选「自定义」：主题色换成自定义的十六进制（默认 #3b82f6），面板展开。整个界面随之按它重新取色，
  // 所以场景标 changesTheme：采集后把变体的外观恢复回来，免得影响同一变体里后面的场景。
  "settings.appearance.custom": {
    module: "settings",
    region: SETTINGS_CONTENT_SCROLLER,
    changesTheme: true,
    async enter({ win }) {
      await openCustomColor(win);
    },
  },
  // 自定义面板展开后滚到内容底：面板把下面的分区顶下去，底部的背景图分区位置随之变化。
  "settings.appearance.custom-bottom": {
    module: "settings",
    region: SETTINGS_CONTENT_SCROLLER,
    changesTheme: true,
    async enter({ win }) {
      await openCustomColor(win);
      await scrollSettingsToEnd(win);
    },
  },
  // 控件状态（B-004）：行悬停、开关键盘聚焦、分段按钮（未选中 / 选中）悬停与键盘聚焦、色板（未选中 / 选中）悬停与键盘聚焦。
  "settings.general.row-hover": {
    module: "settings",
    region: SETTINGS_CONTENT_SCROLLER,
    keepPointer: true,
    async enter({ win }) {
      await hoverBackgroundTasksRow(win);
    },
  },
  "settings.general.switch-focus": {
    module: "settings",
    region: SETTINGS_CONTENT_SCROLLER,
    keepFocus: true,
    async enter({ win }) {
      await focusByKeyboard("general", (w) => w.getByRole("switch", { name: "自动保存", exact: true }))(win);
    },
  },
  "settings.appearance.segment-hover": {
    module: "settings",
    region: SETTINGS_CONTENT_SCROLLER,
    keepPointer: true,
    async enter({ win }) {
      await hoverInAppearance(unselectedSegment)(win);
    },
  },
  "settings.appearance.segment-hover-selected": {
    module: "settings",
    region: SETTINGS_CONTENT_SCROLLER,
    keepPointer: true,
    async enter({ win }) {
      await hoverInAppearance(selectedSegment)(win);
    },
  },
  "settings.appearance.segment-focus": {
    module: "settings",
    region: SETTINGS_CONTENT_SCROLLER,
    keepFocus: true,
    async enter({ win }) {
      await focusByKeyboard("appearance", unselectedSegment)(win);
    },
  },
  "settings.appearance.segment-focus-selected": {
    module: "settings",
    region: SETTINGS_CONTENT_SCROLLER,
    keepFocus: true,
    async enter({ win }) {
      await focusByKeyboard("appearance", selectedSegment)(win);
    },
  },
  "settings.appearance.swatch-hover": {
    module: "settings",
    region: SETTINGS_CONTENT_SCROLLER,
    keepPointer: true,
    async enter({ win }) {
      await hoverInAppearance(customSwatch)(win);
    },
  },
  "settings.appearance.swatch-focus": {
    module: "settings",
    region: SETTINGS_CONTENT_SCROLLER,
    keepFocus: true,
    async enter({ win }) {
      await focusByKeyboard("appearance", customSwatch)(win);
    },
  },
  "settings.appearance.swatch-focus-selected": {
    module: "settings",
    region: SETTINGS_CONTENT_SCROLLER,
    keepFocus: true,
    async enter({ win }) {
      await focusByKeyboard("appearance", selectedSwatch)(win);
    },
  },
  // 控件状态（B-004，续）：下拉触发器、背景图主按钮、取色器触发钮的悬停与键盘聚焦，分段按钮的按下。
  "settings.general.select-hover": {
    module: "settings",
    region: SETTINGS_CONTENT_SCROLLER,
    keepPointer: true,
    async enter({ win }) {
      await openSettingsSection(win, "general");
      await hoverCenter(win, closeActionSelect(win));
    },
  },
  "settings.general.select-focus": {
    module: "settings",
    region: SETTINGS_CONTENT_SCROLLER,
    keepFocus: true,
    async enter({ win }) {
      await focusByKeyboard("general", closeActionSelect)(win);
    },
  },
  "settings.appearance.pick-hover": {
    module: "settings",
    region: SETTINGS_CONTENT_SCROLLER,
    keepPointer: true,
    async enter({ win }) {
      await openSettingsSection(win, "appearance");
      await scrollSettingsTo(win, pickImageButton(win));
      await hoverCenter(win, pickImageButton(win));
    },
  },
  "settings.appearance.pick-focus": {
    module: "settings",
    region: SETTINGS_CONTENT_SCROLLER,
    keepFocus: true,
    async enter({ win }) {
      await focusByKeyboard("appearance", pickImageButton, { scrollTo: true })(win);
    },
  },
  "settings.appearance.segment-pressed": {
    module: "settings",
    region: SETTINGS_CONTENT_SCROLLER,
    keepPointer: true,
    async enter({ win }) {
      await openSettingsSection(win, "appearance");
      await hoverCenter(win, unselectedSegment(win));
      await win.mouse.down();
    },
    // 按着的鼠标移出按钮再松开：不触发点击，页面状态不变。
    async leave({ win }) {
      await win.mouse.move(2, 2);
      await win.mouse.up();
    },
  },
  "settings.appearance.custom-trigger-hover": {
    module: "settings",
    region: SETTINGS_CONTENT_SCROLLER,
    keepPointer: true,
    async enter({ win }) {
      await openCustomColor(win);
      await hoverCenter(win, pickerTrigger(win));
    },
  },
  "settings.appearance.custom-trigger-focus": {
    module: "settings",
    region: SETTINGS_CONTENT_SCROLLER,
    keepFocus: true,
    async enter({ win }) {
      await openCustomColor(win);
      await win.keyboard.press("Tab");
      await pickerTrigger(win).focus();
      await win.mouse.move(2, 2);
    },
  },
  // 十六进制色值框聚焦：全局 input:focus-visible 的 ring-2 ring-primary/50（文本框任何方式聚焦都匹配 :focus-visible）。
  "settings.appearance.hex-focus": {
    module: "settings",
    region: SETTINGS_CONTENT_SCROLLER,
    keepFocus: true,
    async enter({ win }) {
      await openCustomColor(win);
      await win.getByPlaceholder("#3b82f6", { exact: true }).focus();
      await win.mouse.move(2, 2);
    },
  },
  // 整窗 + 取色器弹层（弹层压在 rail 与页面之上，只能在整窗里对照）；主题按自定义色重新取色。
  "app.window.page-settings.custom-picker": {
    module: "app",
    changesTheme: true,
    async enter({ win }) {
      await openCustomPicker(win);
    },
  },
};

export function sceneIds() {
  return Object.keys(SCENES);
}

export function getScene(id) {
  const scene = SCENES[id];
  if (!scene) throw new Error(`未注册的场景 ${id}（已注册：${sceneIds().join("、")}）`);
  return scene;
}
