/**
 * 采集文档站页（M3-6b，DocumentSitePanel）的 React 侧对照场景：初始态、已填入口与目录范围的实际范围预览、
 * 运行中的批次与页面明细、已暂停且有失败项的批次。整窗对照（rail + 顶栏 + 二级侧栏 + 工作区），不隐去任何区域。
 *
 * 数据层是页内替换（lib/react-docsite.mjs）：批次与页面明细取夹具（viva/parity/testdata/docsite-fixture.json）。
 * Viva 一侧（viva/parity/scenes/docsite.go）做同样的操作，最终画面才可比。
 */
import { DOCSITE_FIXTURE, docsiteLabel, enterDocsite, leaveDocsite, titlePattern } from "../lib/react-docsite.mjs";

/** 场景：options 选夹具数据组，steps 把页面带到要对照的状态。 */
function docsiteScene(options, steps) {
  return {
    module: "imports",
    async enter({ win, session }) {
      await enterDocsite(session, options);
      if (steps) await steps(win, session);
    },
    async leave({ session }) {
      await leaveDocsite(session);
    },
  };
}

/**
 * 往标签文字为 label 的输入框里填文字（<label>文字<Input/></label>）。fill 会触发 onChange，实际范围预览随之更新；
 * 采集器采集前统一挪开指针并清焦点，所以不在这里 blur。
 */
const fillField = (label, text) => async (win) => {
  await win.locator("label").filter({ hasText: label }).locator("input").fill(text);
};

/** 点批次标题展开页面明细，等该批次第一页的网址出现（语言无关）。 */
const selectJob = (variantName, jobId) => async (win) => {
  const variant = DOCSITE_FIXTURE.variants[variantName];
  const job = variant.jobs.find((j) => j.id === jobId);
  await win.getByRole("button", { name: titlePattern(job) }).click();
  await win.getByText(variant.pages[jobId][0].url, { exact: false }).first().waitFor({ state: "visible" });
  await win.waitForTimeout(250);
};

export const DOCSITE_SCENES = {
  "app.window.imports.docsite-empty": docsiteScene({ variant: "empty" }),
  "app.window.imports.docsite-scope": docsiteScene({ variant: "empty" }, async (win) => {
    await fillField(docsiteLabel("entryUrl"), DOCSITE_FIXTURE.scope.url)(win);
    await fillField(docsiteLabel("directoryScope"), DOCSITE_FIXTURE.scope.directory)(win);
    await win.waitForTimeout(250);
  }),
  "app.window.imports.docsite-running": docsiteScene({ variant: "running" }, selectJob("running", "crawl-running")),
  "app.window.imports.docsite-failed": docsiteScene({ variant: "failed" }, selectJob("failed", "crawl-paused")),
};
