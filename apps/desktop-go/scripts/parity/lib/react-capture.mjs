/**
 * 快速采集对话框（M3-3 Step 2）对照场景的数据层与操作：夹具、文案、页内替换与对话框的打开。
 *
 * 数据层是页内替换（同 lib/react-settings-core.mjs 的思路，但这里不必赶在渲染层启动之前）：对话框打开时才读合集（collection.list），
 * 点「选择文件」时才读文件选择框（import.selectFiles），都是调用时从 window.api 现取，所以场景进入时在页内换掉这两个接口就够了。
 * 渲染层的组件与 store 一行不改，台账「渲染层改动登记」保持为空。标签列表不换：空库本来就是空的，建议浮层不会出现。
 *
 * 换掉的接口与 store 里缓存的夹具合集会留到后面的场景里，所以场景离开时 reload 一次把页面还原（leaveCapture）。
 * 夹具与 Viva 一侧（viva/parity/scenes/capture.go）是同一份 JSON。可访问名与文案取自渲染层 i18n 目录（settingsText），按 PARITY_LANG 取中文或英文。
 */
import fs from "node:fs";
import path from "node:path";
import { SETTLE_CSS, settle } from "./react-state.mjs";
import { settingsText } from "./react-settings-core.mjs";
import { openSettingsSection } from "./react-settings-nav.mjs";

/** 夹具：与 Viva 场景共用同一份 JSON。 */
export const CAPTURE_FIXTURE = JSON.parse(
  fs.readFileSync(path.join(import.meta.dirname, "../../../viva/parity/testdata/capture-fixture.json"), "utf8"),
);

/** 渲染层 i18n 目录里的一条文案（按 PARITY_LANG 取中文或英文）。 */
export const captureText = settingsText;

const ARMED = "__parity.capture";

/**
 * 在页内换掉两个接口（每次进入场景换一次，离开时 reload 还原）：合集列表取夹具、选择文件框返回 files。
 * 页内函数不能引用外层变量，夹具与文件列表经参数传入。
 */
async function arm(session, files) {
  await session.page.evaluate(
    ({ collections, files: picked, key }) => {
      if (window[key]) return;
      window[key] = true;
      Object.assign(window.api.collection, { list: () => Promise.resolve(JSON.parse(JSON.stringify(collections))) });
      Object.assign(window.api.import, { selectFiles: () => Promise.resolve([...picked]) });
    },
    { collections: CAPTURE_FIXTURE.collections, files, key: ARMED },
  );
}

/**
 * 进入场景：先走到设置页（遮罩背后是它；与 Viva 一侧 settingsWindow 的默认分区一致），换接口，
 * 再像快捷键那样派发 shortcut:newItem 打开对话框，等对话框与草稿框出现。
 */
export async function openCaptureDialog(win, session, { files = [] } = {}) {
  await openSettingsSection(win, "general");
  await arm(session, files);
  await win.evaluate(() => window.dispatchEvent(new CustomEvent("shortcut:newItem")));
  await win.getByRole("dialog").waitFor({ state: "visible" });
  await win.getByTestId("capture-draft").waitFor({ state: "visible" });
}

/** 往草稿框里一次填入文字（整体替换；换行保留）。 */
export async function fillDraft(win, text) {
  await win.getByTestId("capture-draft").fill(text);
}

/** 点「选择文件」：文件框返回 arm 时给的路径，等第一行路径出现。 */
export async function pickFiles(win, firstPath) {
  await win.getByRole("button", { name: captureText("capture.chooseFiles"), exact: true }).click();
  await win.getByText(firstPath, { exact: true }).waitFor({ state: "visible" });
}

/** 在标签框里逐个输入并回车添加。 */
export async function addTags(win, names) {
  const input = win.getByTestId("capture-tags-input");
  for (const name of names) {
    await input.fill(name);
    await input.press("Enter");
    await win.getByText(name, { exact: true }).waitFor({ state: "visible" });
  }
}

/** 在目标合集下拉里选一个合集（选项的名字是合集名）。 */
export async function pickCollection(win, collectionName) {
  await win.getByRole("button", { name: captureText("library.collection"), exact: true }).click();
  await win.getByRole("option", { name: collectionName, exact: true }).click();
  await win.getByRole("listbox").waitFor({ state: "hidden" });
}

/** 离开场景：reload 把换掉的接口、store 里的夹具合集与对话框状态一并收走，再压掉动画。 */
export async function leaveCapture(session) {
  await session.reload();
  await session.page.addStyleTag({ content: SETTLE_CSS });
  await settle(session.page);
}
