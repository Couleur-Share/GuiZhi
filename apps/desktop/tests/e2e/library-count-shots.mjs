import assert from "node:assert/strict";

/**
 * 资料库头部「共 N 个」须与列表条目一致，全程只用正常界面操作。
 * 快速采集的文本经导入队列入库，右键移到回收站走批量命令，两者都由主进程其他服务写库；
 * 空白笔记走条目 IPC 作为对照。每步先截图，最后统一断言，失败时仍留下全部现场。
 */
export default async ({ win, shot }) => {
  await win.evaluate(() => {
    const saved = JSON.parse(localStorage.getItem("guizhi-settings") || '{"state":{}}');
    Object.assign(saved.state, { language: "zh", themeMode: "dark", isDarkMode: true });
    localStorage.setItem("guizhi-settings", JSON.stringify(saved));
    localStorage.setItem("guizhi-setup-dismissed", "1");
    localStorage.setItem("guizhi-migration-dismissed", "1");
  });
  await win.reload();
  const list = win.getByTestId("item-list");
  const counter = win.locator(".library-list-toolbar > span").first();
  await counter.waitFor();
  const waitRows = (count) =>
    win.waitForFunction(
      (expected) => document.querySelectorAll('[data-testid="item-list"] [data-index]').length === expected,
      count,
    );
  const observed = [];
  const record = async (step, expected) => {
    observed.push({ step, expected, text: (await counter.textContent())?.trim() });
    await shot(`library-count-${step}`);
  };

  await win.getByTestId("topbar-new").click();
  await win.getByTestId("capture-draft").fill("计数验收：快速采集的文本笔记\n\n正文用于确认导入队列写库后列表头部的总数。");
  await win.getByTestId("capture-submit").click();
  await waitRows(1);
  await record("capture", 1);

  await win.getByTestId("topbar-new").click();
  await win.getByTestId("capture-blank-note").click();
  await waitRows(2);
  await record("blank-note", 2);

  await list.locator("[data-index] button").filter({ hasNotText: "无标题" }).click({ button: "right" });
  await win.getByRole("menuitem", { name: "移到回收站", exact: true }).click();
  await waitRows(1);
  await record("trash", 1);

  console.log(JSON.stringify(observed));
  for (const { step, expected, text } of observed) {
    assert.equal(text, `共 ${expected} 个`, `${step} 后列表有 ${expected} 条，头部计数不一致`);
  }
};
