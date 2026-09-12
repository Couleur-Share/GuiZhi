import reader from "./reading-v3-reader.mjs";
import security from "./reading-v3-shots.mjs";
import savedOutput from "./reading-v3-real-output.mjs";

/** 同一隔离源码验收 UI、渐进阅读与运行边界，全部使用固定资料，不请求模型。 */
export default async (context) => {
  await reader(context);
  // 卸载当前阅读组件，释放其独立视图，再运行底层边界夹具。
  await context.win.reload();
  await context.win.waitForTimeout(700);
  await security(context);
  if (process.env.GUIZHI_READING_SAVED_OUTPUT) await savedOutput(context);
};
