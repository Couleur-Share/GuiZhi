import fs from "fs";
import os from "os";
import path from "path";
import { afterEach, describe, expect, it } from "vitest";
import {
  captureWindowState,
  fitBoundsToDisplays,
  readWindowLaunchState,
  resolveLaunchVisibility,
  resolveWindowLaunchState,
  writeWindowState,
} from "../../../src/main/window-state";

const DISPLAY = { x: 0, y: 0, width: 1920, height: 1040 };
const tempDirs: string[] = [];

afterEach(() => {
  for (const dir of tempDirs.splice(0)) {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

describe("window state", () => {
  it("maximizes the first normal launch", () => {
    expect(resolveWindowLaunchState(null, [DISPLAY])).toEqual({
      bounds: { width: 1200, height: 800 },
      shouldMaximize: true,
    });
  });

  it("restores normal bounds and maximized state", () => {
    expect(
      resolveWindowLaunchState(
        {
          version: 1,
          bounds: { x: 120, y: 80, width: 1400, height: 900 },
          isMaximized: true,
        },
        [DISPLAY],
      ),
    ).toEqual({
      bounds: { x: 120, y: 80, width: 1400, height: 900 },
      shouldMaximize: true,
    });
  });

  it("falls back to maximized when the saved monitor disappeared", () => {
    expect(
      resolveWindowLaunchState(
        {
          version: 1,
          bounds: { x: 2400, y: 100, width: 1200, height: 800 },
          isMaximized: false,
        },
        [DISPLAY],
      ),
    ).toEqual({
      bounds: { width: 1200, height: 800 },
      shouldMaximize: true,
    });
  });

  it("clamps oversized bounds to the matching work area", () => {
    expect(
      fitBoundsToDisplays({ x: -100, y: -80, width: 2400, height: 1400 }, [
        DISPLAY,
      ]),
    ).toEqual(DISPLAY);
  });

  it("does not persist transient fullscreen state", () => {
    expect(
      captureWindowState({
        getNormalBounds: () => ({ x: 10, y: 20, width: 1200, height: 800 }),
        isMaximized: () => false,
        isFullScreen: () => true,
      }),
    ).toBeNull();
  });

  it("keeps the normal bounds while persisting a maximized window", () => {
    expect(
      captureWindowState({
        getNormalBounds: () => ({ x: 80, y: 40, width: 1280, height: 800 }),
        isMaximized: () => true,
        isFullScreen: () => false,
      }),
    ).toEqual({
      version: 1,
      bounds: { x: 80, y: 40, width: 1280, height: 800 },
      isMaximized: true,
    });
  });

  it("shows the window on a manual launch even when minimizeOnLaunch is on", () => {
    expect(
      resolveLaunchVisibility({
        argv: ["GuiZhi.exe"],
        openedAtLogin: false,
        minimizeOnLaunch: true,
        shouldMaximize: false,
      }),
    ).toEqual({ startHidden: false, maximizeNow: false, maximizeOnFirstShow: false });
  });

  it("starts hidden when the login item passes --hidden", () => {
    expect(
      resolveLaunchVisibility({
        argv: ["GuiZhi.exe", "--hidden"],
        openedAtLogin: false,
        minimizeOnLaunch: false,
        shouldMaximize: false,
      }).startHidden,
    ).toBe(true);
  });

  it("starts hidden for a macOS login launch only when the setting is on", () => {
    const base = { argv: ["GuiZhi"], openedAtLogin: true, shouldMaximize: false };
    expect(resolveLaunchVisibility({ ...base, minimizeOnLaunch: true }).startHidden).toBe(true);
    expect(resolveLaunchVisibility({ ...base, minimizeOnLaunch: false }).startHidden).toBe(false);
  });

  it("defers maximize until first show when starting hidden", () => {
    expect(
      resolveLaunchVisibility({
        argv: ["GuiZhi.exe", "--hidden"],
        openedAtLogin: false,
        minimizeOnLaunch: true,
        shouldMaximize: true,
      }),
    ).toEqual({ startHidden: true, maximizeNow: false, maximizeOnFirstShow: true });
    expect(
      resolveLaunchVisibility({
        argv: ["GuiZhi.exe"],
        openedAtLogin: false,
        minimizeOnLaunch: true,
        shouldMaximize: true,
      }),
    ).toEqual({ startHidden: false, maximizeNow: true, maximizeOnFirstShow: false });
  });

  it("round-trips a valid state file", () => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-window-state-"));
    tempDirs.push(dir);
    const stateFile = path.join(dir, "window-state.json");
    writeWindowState(stateFile, {
      version: 1,
      bounds: { x: 40, y: 60, width: 1280, height: 800 },
      isMaximized: false,
    });

    expect(readWindowLaunchState(stateFile, [DISPLAY])).toEqual({
      bounds: { x: 40, y: 60, width: 1280, height: 800 },
      shouldMaximize: false,
    });
  });
});
