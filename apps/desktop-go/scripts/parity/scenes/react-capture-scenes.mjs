/**
 * 快速采集对话框（M3-3 Step 2）的 React 侧对照场景：空草稿、输入链接、分享口令（混合内容）、文件加标签加合集。
 *
 * 都是整窗对照（遮罩与面板压在 rail 与顶栏之上，背后是设置页）。对话框经 shortcut:newItem 打开（与快捷键、托盘同一入口）。
 * 数据层与操作在 lib/react-capture.mjs；夹具与 Viva 一侧（viva/parity/scenes/capture.go）是同一份 JSON。
 * 草稿框的焦点就在框里（autoFocus），采集前不清焦点（keepFocus）：焦点在框里是用户看到的状态，Viva 一侧同样留在框里。
 */
import {
  CAPTURE_FIXTURE as FIX,
  addTags,
  fillDraft,
  leaveCapture,
  openCaptureDialog,
  pickCollection,
  pickFiles,
} from "../lib/react-capture.mjs";

/** 场景：steps 在对话框打开之后把它带到要对照的状态。 */
function captureScene(steps, { files = [] } = {}) {
  return {
    module: "settings",
    keepFocus: true,
    async enter({ win, session }) {
      await openCaptureDialog(win, session, { files });
      if (steps) await steps(win);
    },
    async leave({ session }) {
      await leaveCapture(session);
    },
  };
}

const FILES_TAGS = FIX.scenes.filesTags;
const collectionName = (id) => FIX.collections.find((c) => c.id === id).name;

export const CAPTURE_SCENES = {
  // 空草稿：占位文字、提示「支持文本、链接与文本文件」、「采集」禁用。
  "capture.dlg.empty": captureScene(null),
  // 两行链接：状态栏变绿、地球图标、「识别到 2 个链接」，「采集」可用。
  "capture.dlg.urls": captureScene((win) => fillDraft(win, FIX.scenes.urls.draft)),
  // 分享口令：文字里夹着有专用连接器的链接，默认采集链接，状态栏右侧出现改判按钮。
  "capture.dlg.mixed": captureScene((win) => fillDraft(win, FIX.scenes.mixed.draft)),
  // 三个文件、两个标签、选了合集（草稿为空，靠文件使「采集」可用）。
  "capture.dlg.files-tags": captureScene(
    async (win) => {
      await pickFiles(win, FILES_TAGS.files[0]);
      await addTags(win, FILES_TAGS.tags);
      await pickCollection(win, collectionName(FILES_TAGS.collection));
    },
    { files: FILES_TAGS.files },
  ),
};
