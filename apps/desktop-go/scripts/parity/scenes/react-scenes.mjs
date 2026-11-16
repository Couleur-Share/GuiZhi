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

/** 设置内容滚动容器：内容壳的直接父元素（没有 testid，用结构选择器）。 */
const SETTINGS_CONTENT_SCROLLER = 'div:has(> [data-testid="settings-content-shell"])';

export const SCENES = {
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
