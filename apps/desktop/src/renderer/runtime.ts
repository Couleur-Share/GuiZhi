export interface RuntimeCapabilities {
  appUpdate: boolean;
  desktopWindowControls: boolean;
}

type WebRuntimeWindow = Window &
  typeof globalThis & {
    __GUIZHI_WEB__?: boolean;
  };

function getRuntimeWindow(): WebRuntimeWindow | undefined {
  return typeof window === "undefined"
    ? undefined
    : (window as WebRuntimeWindow);
}

/**
 * 归知当前仅有桌面形态；保留 Web 运行时判定接口以便未来扩展，
 * 桌面环境恒为 false。
 */
export function isWebRuntime(): boolean {
  return getRuntimeWindow()?.__GUIZHI_WEB__ === true;
}

export function getRuntimeCapabilities(): RuntimeCapabilities {
  if (isWebRuntime()) {
    return {
      appUpdate: false,
      desktopWindowControls: false,
    };
  }

  return {
    appUpdate: true,
    desktopWindowControls: true,
  };
}
