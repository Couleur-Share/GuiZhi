/**
 * React 侧场景注册表：每个场景知道怎样从任意状态走到自己、取哪个区域。
 *
 * 场景 ID 与盘点清单（docs/agents/viva-parity-inventory/）一致：`<模块>.<屏幕>[.<状态>]`。
 * `enter(ctx)` 必须是幂等的——从任意界面状态出发都能走到该场景（同一会话里连续采集多个场景时，上一个场景留下的状态不能影响下一个）。
 * `region`：阶段 1 校准用的区域场景（取该元素的外框，Viva 侧在同尺寸画布里只画这块）；缺省是整个视口。
 *
 * 渲染层只允许加 data-testid / aria 属性（见台账「渲染层改动登记」）；能用现有角色与名称定位的，不去改渲染层。
 */

/** 打开设置并切到某个分区，等内容壳出现。 */
async function openSettingsSection(win, section) {
  await win.getByTestId("rail-settings").click();
  await win.getByTestId(`settings-nav-${section}`).click();
  await win.getByTestId("settings-content-shell").waitFor({ state: "visible" });
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

/** 设置内容滚动容器：内容壳的直接父元素（没有 testid，用结构选择器）。 */
const SETTINGS_CONTENT_SCROLLER = 'div:has(> [data-testid="settings-content-shell"])';

/** 设置页外壳（侧栏 + 内容）：SettingsPage 的根元素，即内容滚动容器的父元素。 */
const SETTINGS_SHELL = 'div:has(> div > [data-testid="settings-content-shell"])';

export const SCENES = {
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
};

export function sceneIds() {
  return Object.keys(SCENES);
}

export function getScene(id) {
  const scene = SCENES[id];
  if (!scene) throw new Error(`未注册的场景 ${id}（已注册：${sceneIds().join("、")}）`);
  return scene;
}
