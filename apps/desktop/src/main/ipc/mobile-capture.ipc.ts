import { ipcMain } from "electron";
import type Database from "@guizhi/db/adapter";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import { getRegisteredImportService } from "./import.ipc";
import { CaptureReceiver } from "../services/mobile-capture/receiver";
import { setMobileCaptureStop } from "../services/mobile-capture/lifecycle";
export function registerMobileCaptureIPC(db: Database.Database) {
  const receiver = new CaptureReceiver(db, getRegisteredImportService());
  setMobileCaptureStop(pause => receiver.stop(pause));
  ipcMain.removeHandler(IPC_CHANNELS.MOBILE_CAPTURE);
  ipcMain.handle(IPC_CHANNELS.MOBILE_CAPTURE, async (_event, action: string, args: Record<string, unknown> = {}) => {
    try {
      let data: unknown;
      switch (action) {
        case "status": data = receiver.status(); break;
        case "activate":
          if (typeof args.origin !== "string" || typeof args.invite !== "string") throw new Error("激活参数无效");
          data = await receiver.activate(args.origin, args.invite); break;
        case "configure":
          if (typeof args.paused !== "boolean" || (args.collectionId !== null && typeof args.collectionId !== "string")) throw new Error("设置参数无效");
          data = receiver.configure(args.paused, args.collectionId as string | null); break;
        case "pair": data = await receiver.pairing(); break;
        case "pairings": data = await receiver.pairings(); break;
        case "devices": data = await receiver.devices(); break;
        case "confirm":
          if (typeof args.id !== "string" || typeof args.deviceId !== "string") throw new Error("配对参数无效");
          data = await receiver.confirm(args.id, args.deviceId); break;
        case "revoke":
          if (typeof args.id !== "string") throw new Error("设备编号无效");
          data = await receiver.revoke(args.id); break;
        case "disable": data = await receiver.disable(); break;
        case "fetch": await receiver.tick(); data = receiver.status(); break;
        default: throw new Error("不支持的手机收集操作");
      }
      return { success: true, data };
    } catch (error) { return { success: false, error: error instanceof Error ? error.message : "手机收集操作失败" }; }
  });
}
