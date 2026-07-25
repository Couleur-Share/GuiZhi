/**
 * 用户通过文件选择器亲自选中的可执行文件路径。
 *
 * `ytDlpPath` / `ffmpegPath` 最终会被主进程 spawn。如果渲染进程可以往
 * settings 里写任意路径，一次 XSS 就能升格成主进程任意代码执行——而归知
 * 渲染的正是导入进来的第三方网页内容。
 *
 * 因此这两个键只接受本会话里由 `dialog.showOpenDialog` 返回的路径：
 * 那意味着用户在系统弹窗里亲手点过这个文件。已保存在库里的旧值不受影响。
 */
const pickedPaths = new Set<string>();

export function rememberPickedBinaryPath(filePath: string): void {
  if (filePath.trim()) {
    pickedPaths.add(filePath);
  }
}

/** 空值表示「清除自定义路径」，始终允许 */
export function isAcceptableBinaryPath(filePath: unknown): boolean {
  if (typeof filePath !== "string") {
    return false;
  }
  return filePath.trim() === "" || pickedPaths.has(filePath);
}

/** 仅供测试重置 */
export function resetPickedBinaryPaths(): void {
  pickedPaths.clear();
}
