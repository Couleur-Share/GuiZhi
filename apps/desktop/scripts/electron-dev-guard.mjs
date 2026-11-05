/**
 * Electron 开发态构建守卫（dev build guard）。
 *
 * 背景：vite-plugin-electron 在各入口的 closeBundle 中调用 onstart，而 Rollup 在构建
 * 失败时同样会触发 closeBundle；Vite 监听重建又会在开始前清空 outDir。于是 main 的
 * 一次构建错误会先 taskkill 正在运行的应用，再用已被清空的 out/main 重新拉起 Electron，
 * 弹出 “Unable to find Electron app” 并带着整个 dev 会话退出。
 * Background: vite-plugin-electron runs onstart from closeBundle, Rollup fires
 * closeBundle even when a build fails, and Vite empties outDir before every watch
 * rebuild — so a single main-process build error kills the running app and relaunches
 * Electron against an empty out/main.
 *
 * 做法：记录入口最近一次构建是否成功；失败时跳过重启/重载并在终端留下提示，下一次
 * 成功的重建会自动恢复。只影响 dev 的 onstart，不改变任何构建产物。
 * Approach: remember whether the latest build of an entry succeeded and skip the
 * restart/reload when it did not; the next successful rebuild recovers by itself.
 * Build output is untouched.
 *
 * @param {{ label: string, skipped: string, log?: (message: string) => void }} options
 *   label：入口名，用于插件名与提示；skipped：构建失败时被跳过的动作；log：提示输出，默认 stderr。
 */
export function createDevBuildGuard({ label, skipped, log = console.error }) {
  let failed = false;
  return {
    /** 放进该入口的 vite.plugins，在 Rollup 生命周期里记录构建成败。 */
    plugin: {
      name: `guizhi-dev-build-guard:${label}`,
      buildStart() {
        failed = false;
      },
      // 构建失败时 Rollup 先以 error 调用 buildEnd，再触发 closeBundle（即 onstart）。
      buildEnd(error) {
        failed = Boolean(error);
      },
    },
    /** 仅当最近一次构建成功时才执行 action（重启或重载）；返回是否已执行。 */
    run(action) {
      if (failed) {
        log(
          `[guizhi] ${label} 构建失败：已跳过${skipped}，正在运行的应用不受影响；修复错误或重新保存相关文件后会自动重建。`,
        );
        return false;
      }
      action();
      return true;
    },
  };
}
