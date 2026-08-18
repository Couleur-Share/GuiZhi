import fs from "fs";
import path from "path";
import type { BrowserWindow } from "electron";

export const DEFAULT_WINDOW_BOUNDS = {
  width: 1200,
  height: 800,
} as const;

export const MIN_WINDOW_BOUNDS = {
  width: 800,
  height: 600,
} as const;

const WINDOW_STATE_VERSION = 1;
const SAVE_DEBOUNCE_MS = 250;

export interface WindowBounds {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface PersistedWindowState {
  version: typeof WINDOW_STATE_VERSION;
  bounds: WindowBounds;
  isMaximized: boolean;
}

export interface WindowLaunchState {
  bounds: Partial<WindowBounds> & Pick<WindowBounds, "width" | "height">;
  shouldMaximize: boolean;
}

interface WindowStateSource {
  getNormalBounds: () => WindowBounds;
  isMaximized: () => boolean;
  isFullScreen: () => boolean;
}

function isFiniteNumber(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value);
}

function parsePersistedWindowState(
  value: unknown,
): PersistedWindowState | null {
  if (!value || typeof value !== "object") return null;
  const candidate = value as Partial<PersistedWindowState>;
  const bounds = candidate.bounds as Partial<WindowBounds> | undefined;
  if (
    candidate.version !== WINDOW_STATE_VERSION ||
    typeof candidate.isMaximized !== "boolean" ||
    !bounds ||
    !isFiniteNumber(bounds.x) ||
    !isFiniteNumber(bounds.y) ||
    !isFiniteNumber(bounds.width) ||
    !isFiniteNumber(bounds.height) ||
    bounds.width < MIN_WINDOW_BOUNDS.width ||
    bounds.height < MIN_WINDOW_BOUNDS.height
  ) {
    return null;
  }
  return {
    version: WINDOW_STATE_VERSION,
    bounds: {
      x: Math.round(bounds.x),
      y: Math.round(bounds.y),
      width: Math.round(bounds.width),
      height: Math.round(bounds.height),
    },
    isMaximized: candidate.isMaximized,
  };
}

function overlapArea(left: WindowBounds, right: WindowBounds): number {
  const width = Math.max(
    0,
    Math.min(left.x + left.width, right.x + right.width) -
      Math.max(left.x, right.x),
  );
  const height = Math.max(
    0,
    Math.min(left.y + left.height, right.y + right.height) -
      Math.max(left.y, right.y),
  );
  return width * height;
}

/**
 * 把旧窗口放回与它重叠最多的屏幕。显示器被拔掉时没有重叠，交给首次启动兜底最大化。
 */
export function fitBoundsToDisplays(
  bounds: WindowBounds,
  displayWorkAreas: readonly WindowBounds[],
): WindowBounds | null {
  const target = displayWorkAreas
    .map((workArea) => ({ workArea, overlap: overlapArea(bounds, workArea) }))
    .sort((left, right) => right.overlap - left.overlap)[0];
  if (!target || target.overlap === 0) return null;

  const width = Math.min(
    Math.max(bounds.width, MIN_WINDOW_BOUNDS.width),
    target.workArea.width,
  );
  const height = Math.min(
    Math.max(bounds.height, MIN_WINDOW_BOUNDS.height),
    target.workArea.height,
  );
  const maxX = target.workArea.x + target.workArea.width - width;
  const maxY = target.workArea.y + target.workArea.height - height;
  return {
    x: Math.min(Math.max(bounds.x, target.workArea.x), maxX),
    y: Math.min(Math.max(bounds.y, target.workArea.y), maxY),
    width,
    height,
  };
}

export function resolveWindowLaunchState(
  persisted: unknown,
  displayWorkAreas: readonly WindowBounds[],
): WindowLaunchState {
  const state = parsePersistedWindowState(persisted);
  const fitted = state
    ? fitBoundsToDisplays(state.bounds, displayWorkAreas)
    : null;
  if (!state || !fitted) {
    return {
      bounds: DEFAULT_WINDOW_BOUNDS,
      shouldMaximize: true,
    };
  }
  return {
    bounds: fitted,
    shouldMaximize: state.isMaximized,
  };
}

export function readWindowLaunchState(
  stateFile: string,
  displayWorkAreas: readonly WindowBounds[],
): WindowLaunchState {
  let persisted: unknown;
  try {
    persisted = JSON.parse(fs.readFileSync(stateFile, "utf8"));
  } catch {
    persisted = null;
  }
  return resolveWindowLaunchState(persisted, displayWorkAreas);
}

export function captureWindowState(
  window: WindowStateSource,
): PersistedWindowState | null {
  if (window.isFullScreen()) return null;
  const parsed = parsePersistedWindowState({
    version: WINDOW_STATE_VERSION,
    bounds: window.getNormalBounds(),
    isMaximized: window.isMaximized(),
  });
  return parsed;
}

export function writeWindowState(
  stateFile: string,
  state: PersistedWindowState,
): void {
  fs.mkdirSync(path.dirname(stateFile), { recursive: true });
  fs.writeFileSync(stateFile, `${JSON.stringify(state)}\n`, "utf8");
}

/**
 * 移动/缩放期间合并频繁事件；关闭前同步落盘，保证托盘退出与正常退出都能恢复。
 */
export function attachWindowStatePersistence(
  window: BrowserWindow,
  stateFile: string,
): void {
  let saveTimer: ReturnType<typeof setTimeout> | null = null;

  const save = () => {
    saveTimer = null;
    if (window.isDestroyed()) return;
    try {
      const state = captureWindowState(window);
      if (state) writeWindowState(stateFile, state);
    } catch (error) {
      console.warn(
        "Failed to persist window state:",
        error instanceof Error ? error.message : error,
      );
    }
  };
  const scheduleSave = () => {
    if (saveTimer) clearTimeout(saveTimer);
    saveTimer = setTimeout(save, SAVE_DEBOUNCE_MS);
  };
  const flushSave = () => {
    if (saveTimer) {
      clearTimeout(saveTimer);
      saveTimer = null;
    }
    save();
  };

  window.on("resize", scheduleSave);
  window.on("move", scheduleSave);
  window.on("maximize", scheduleSave);
  window.on("unmaximize", scheduleSave);
  window.on("close", flushSave);
}
