/**
 * 设置页的导航辅助：打开设置并切分区、复位与滚动内容区。
 * 从 scenes/react-scenes.mjs 抽出，让按分区拆分的场景文件（如 react-settings-core-scenes.mjs）共用同一套进入方式。
 */

/** 设置内容滚动容器：内容壳的直接父元素（没有 testid，用结构选择器）。 */
export const SETTINGS_CONTENT_SCROLLER = 'div:has(> [data-testid="settings-content-shell"])';

/** 打开设置并切到某个分区，等内容壳出现。 */
export async function openSettingsSection(win, section) {
  // 上一个场景可能留下了模态对话框（遮罩会拦住后面的点击）：Esc 先把它与残留的浮层收起。
  await win.keyboard.press("Escape");
  await win.getByTestId("rail-settings").click();
  await win.getByTestId(`settings-nav-${section}`).click();
  await win.getByTestId("settings-content-shell").waitFor({ state: "visible" });
  // 同一分区再次进入时滚动容器不会重建：上一个场景（滚到底的那些）留下的滚动位置要复位，场景才与顺序无关。
  await win.locator(SETTINGS_CONTENT_SCROLLER).evaluate((el) => {
    el.scrollTop = 0;
  });
}

/** 把设置内容滚到底（与 Viva 侧的 parity.ScrollDown 对应）。 */
export async function scrollSettingsToEnd(win) {
  await win.locator(SETTINGS_CONTENT_SCROLLER).evaluate((el) => {
    el.scrollTop = el.scrollHeight;
  });
}
