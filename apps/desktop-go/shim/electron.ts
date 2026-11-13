// `electron` 模块的替身：让归知现有的 preload（215 处 invoke、20 处 send、13 处 on）
// 原样运行在 MyGo 的页面里。invoke/send 统一走 Go 侧一个绑定方法 Bridge.Invoke / Bridge.Send，
// Go 推送的事件走 "ipc" 事件再按频道分发给 on 监听器。
import { readingViewInvoke } from "./reading-view-host";
import { installMediaUrlRewrite } from "./media-urls";

// local-image:// / local-video:// 在 WebView2 里要改写成 http://<scheme>.localhost/，见 media-urls.ts
installMediaUrlRewrite();

type Listener = (event: unknown, ...args: any[]) => void;

// preload 回调的首个参数类型。Go 壳不传递真实事件对象（始终是 {}），保留该名字让 preload 源码的类型标注不变。
export interface IpcRendererEvent {}

declare global {
  interface Window {
    mygo: {
      call<T = unknown>(method: string, ...args: unknown[]): Promise<T>;
      on<T = unknown>(name: string, fn: (payload: T) => void): () => void;
    };
  }
}

const listeners = new Map<string, Set<Listener>>();
let subscribed = false;

function ensureSubscribed() {
  if (subscribed) return;
  subscribed = true;
  window.mygo.on<{ channel: string; args: unknown[] }>("ipc", ({ channel, args }) => {
    for (const fn of listeners.get(channel) ?? []) fn({}, ...(args ?? []));
  });
}

export const ipcRenderer = {
  async invoke(channel: string, ...args: unknown[]) {
    const raw = async (ch: string, a: unknown[]) => {
      const reply = await window.mygo.call<string>("Bridge.Invoke", ch, JSON.stringify(a));
      return reply === "" ? undefined : JSON.parse(reply);
    };
    // 主题阅读视图要落到页面 DOM 上，由 reading-view-host 在 Go 校验后接手
    if (channel.startsWith("themedReading:view")) {
      const r = await readingViewInvoke(channel, args, raw, (c, fn) => ipcRenderer.on(c, fn), (c, ...a) => ipcRenderer.send(c, ...a));
      if (r.handled) return r.value;
    }
    return raw(channel, args);
  },
  send(channel: string, ...args: unknown[]) {
    void window.mygo.call("Bridge.Send", channel, JSON.stringify(args));
  },
  on(channel: string, fn: Listener) {
    ensureSubscribed();
    if (!listeners.has(channel)) listeners.set(channel, new Set());
    listeners.get(channel)!.add(fn);
    return ipcRenderer;
  },
  removeListener(channel: string, fn: Listener) {
    listeners.get(channel)?.delete(fn);
    return ipcRenderer;
  },
  removeAllListeners(channel: string) {
    listeners.delete(channel);
    return ipcRenderer;
  },
};

// contextBridge 的作用是把对象放进页面主世界；MyGo 没有隔离世界，直接挂到 window。
export const contextBridge = {
  exposeInMainWorld(name: string, value: unknown) {
    (window as any)[name] = value;
  },
};

// File 的真实路径：MyGo 通过文件拖放事件提供，spike 阶段先不实现。
export const webUtils = {
  getPathForFile(_file: File): string {
    return "";
  },
};

// Electron 的窗口拖拽区域写作 `-webkit-app-region`，MyGo 认的是 `--app-region`。
// 渲染进程的 CSS 不改：页面加载后扫描样式表，把带该属性的规则同样写成 `--app-region`（幂等，只维护一个 <style>）。
function mirrorAppRegion() {
  const rules: string[] = [];
  for (const sheet of Array.from(document.styleSheets)) {
    let list: CSSRuleList;
    try {
      list = sheet.cssRules; // 跨域样式表会抛 SecurityError，渲染进程的样式都同源
    } catch {
      continue;
    }
    for (const rule of Array.from(list)) {
      if (!(rule instanceof CSSStyleRule)) continue;
      const value = rule.style.getPropertyValue("-webkit-app-region").trim();
      if (value) rules.push(`${rule.selectorText}{--app-region:${value}}`);
    }
  }
  const id = "guizhi-mygo-app-region";
  let el = document.getElementById(id) as HTMLStyleElement | null;
  if (!el) {
    el = document.createElement("style");
    el.id = id;
    document.head.appendChild(el);
  }
  el.textContent = rules.join("\n");
}
window.addEventListener("DOMContentLoaded", mirrorAppRegion);
window.addEventListener("load", mirrorAppRegion);