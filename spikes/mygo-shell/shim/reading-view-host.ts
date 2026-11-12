// 主题阅读视图的页面内宿主：Electron 用 WebContentsView 叠在主窗口上，MyGo 用一个固定定位的
// <iframe sandbox="allow-scripts">（srcdoc = Go 生成的外壳文档）。外壳内再嵌一层无同源权限的 iframe 跑模型脚本。
// 校验、心跳、故障判定全在 Go（reading_views.go）；这里只做三件事：
//   1. 创建 / 更新位置 / 销毁 iframe（位置已由 Go 校验）；
//   2. 把 Go 推来的 readingView:command 转给 iframe；
//   3. 把 iframe 来的 postMessage（带视图 ID 与来源校验）经 readingView:bridge 交给 Go。
type Invoke = (channel: string, args: unknown[]) => Promise<any>;

const frames = new Map<string, HTMLIFrameElement>();
let installed = false;

function install(on: (channel: string, fn: (event: unknown, ...args: any[]) => void) => void, send: (channel: string, ...args: unknown[]) => void) {
  if (installed) return;
  installed = true;
  window.addEventListener("message", (event) => {
    const data = event.data as { gzReadingView?: unknown; data?: { reading?: unknown; type?: unknown; value?: unknown } } | null;
    if (!data || typeof data.gzReadingView !== "string" || !data.data || data.data.reading !== 3) return;
    const frame = frames.get(data.gzReadingView);
    // 只接受该视图自己的 iframe：来源窗口必须就是它
    if (!frame || event.source !== frame.contentWindow) return;
    send("readingView:bridge", { viewId: data.gzReadingView, type: data.data.type, value: data.data.value });
  });
  on("readingView:command", (_e, payload: { viewId?: string; command?: unknown }) => {
    const frame = payload?.viewId ? frames.get(payload.viewId) : undefined;
    frame?.contentWindow?.postMessage({ gzReadingCommand: payload.command, viewId: payload.viewId }, "*");
  });
  on("themedReading:viewEvent", (_e, payload: { viewId?: string; type?: string }) => {
    // 故障：立刻移除实例（渲染进程随后回落静态正文）
    if (payload?.type === "fault" && payload.viewId) removeFrame(payload.viewId);
  });
}

function removeFrame(id: string) {
  const frame = frames.get(id);
  if (!frame) return;
  frames.delete(id);
  frame.srcdoc = "";
  frame.remove();
}

function applyBounds(frame: HTMLIFrameElement, b: { x: number; y: number; width: number; height: number; visible: boolean }) {
  // 与 Electron 版一样裁剪到窗口内容区
  const x = Math.max(0, Math.min(Math.round(b.x), window.innerWidth));
  const y = Math.max(0, Math.min(Math.round(b.y), window.innerHeight));
  const w = Math.max(0, Math.min(Math.round(b.width), window.innerWidth - x));
  const h = Math.max(0, Math.min(Math.round(b.height), window.innerHeight - y));
  const s = frame.style;
  s.left = `${x}px`;
  s.top = `${y}px`;
  s.width = `${w}px`;
  s.height = `${h}px`;
  s.display = b.visible && w > 0 && h > 0 ? "block" : "none";
}

/** 包装 invoke：视图相关频道在 Go 校验通过后，由这里落实到 DOM。其余频道原样放行（返回 undefined 表示未处理）。 */
export async function readingViewInvoke(
  channel: string,
  args: unknown[],
  call: Invoke,
  on: (channel: string, fn: (event: unknown, ...args: any[]) => void) => void,
  send: (channel: string, ...args: unknown[]) => void,
): Promise<{ handled: boolean; value?: any }> {
  switch (channel) {
    case "themedReading:viewCreate": {
      install(on, send);
      const reply = await call(channel, args);
      if (reply?.success && typeof reply.document === "string" && typeof reply.viewId === "string") {
        const frame = document.createElement("iframe");
        frame.setAttribute("sandbox", "allow-scripts");
        frame.setAttribute("referrerpolicy", "no-referrer");
        frame.setAttribute("title", "AI 阅读页");
        Object.assign(frame.style, { position: "fixed", border: "0", margin: "0", padding: "0", left: "0", top: "0", width: "0", height: "0", display: "none", zIndex: "40", background: "transparent" });
        frame.srcdoc = reply.document;
        document.body.appendChild(frame);
        frames.set(reply.viewId, frame);
        delete reply.document; // 文档只给宿主，不让渲染进程业务代码看到
      }
      return { handled: true, value: reply };
    }
    case "themedReading:viewUpdate": {
      const reply = await call(channel, args);
      const req = args[0] as { viewId?: string; bounds?: any } | undefined;
      const frame = req?.viewId ? frames.get(req.viewId) : undefined;
      if (reply?.success && frame && req?.bounds) applyBounds(frame, req.bounds);
      return { handled: true, value: reply };
    }
    case "themedReading:viewCommand": {
      const reply = await call(channel, args);
      const req = args[0] as { viewId?: string } | undefined;
      const frame = req?.viewId ? frames.get(req.viewId) : undefined;
      if (reply?.success && typeof reply.document === "string" && frame) {
        frame.srcdoc = reply.document; // 切换交互：重新加载文档
        delete reply.document;
      }
      return { handled: true, value: reply };
    }
    case "themedReading:viewDestroy": {
      const id = args[0];
      const reply = await call(channel, args);
      if (typeof id === "string") removeFrame(id);
      return { handled: true, value: reply };
    }
  }
  return { handled: false };
}