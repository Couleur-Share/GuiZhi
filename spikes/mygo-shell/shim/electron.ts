// `electron` 模块的替身：让归知现有的 preload（215 处 invoke、20 处 send、13 处 on）
// 原样运行在 MyGo 的页面里。invoke/send 统一走 Go 侧一个绑定方法 Bridge.Invoke / Bridge.Send，
// Go 推送的事件走 "ipc" 事件再按频道分发给 on 监听器。
type Listener = (event: unknown, ...args: any[]) => void;

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
    const reply = await window.mygo.call<string>("Bridge.Invoke", channel, JSON.stringify(args));
    return reply === "" ? undefined : JSON.parse(reply);
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
