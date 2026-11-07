import { BrowserWindow, ipcMain, dialog } from "electron";
import type { IpcMainInvokeEvent, OpenDialogOptions } from "electron";
import { randomUUID } from "crypto";
import path from "path";
import fs from "fs/promises";
import { IPC_CHANNELS } from "@guizhi/shared/constants";
import { getImagesDir } from "../runtime-paths";

const IMAGE_EXTENSIONS = new Set([".jpg", ".jpeg", ".png", ".gif", ".webp"]);

let lastSelectedImagePaths = new Set<string>();

function isAllowedSelectedImagePath(filePath: string): boolean {
  return lastSelectedImagePaths.has(path.resolve(filePath));
}

/**
 * Validate filename to prevent path traversal.
 */
function validateFileName(fileName: string, baseDir: string): string {
  if (
    typeof fileName !== "string" ||
    fileName.length === 0 ||
    fileName === "." ||
    fileName === ".." ||
    fileName.includes("..") ||
    /[/\\:\u0000-\u001F\u007F]/u.test(fileName)
  ) {
    throw new Error("Invalid filename: path traversal detected");
  }

  const resolvedBase = path.resolve(baseDir);
  const fullPath = path.resolve(resolvedBase, fileName);
  const relative = path.relative(resolvedBase, fullPath);

  if (
    relative === "" ||
    relative.startsWith("..") ||
    path.isAbsolute(relative)
  ) {
    throw new Error("Invalid filename: path traversal detected");
  }

  return fullPath;
}

async function pathExists(filePath: string): Promise<boolean> {
  try {
    await fs.access(filePath);
    return true;
  } catch {
    return false;
  }
}

function showSenderOwnedOpenDialog(
  event: Pick<IpcMainInvokeEvent, "sender"> | null | undefined,
  options: OpenDialogOptions,
) {
  const parent = event?.sender
    ? BrowserWindow.fromWebContents(event.sender)
    : null;
  return parent
    ? dialog.showOpenDialog(parent, options)
    : dialog.showOpenDialog(options);
}

/**
 * 本地图片：外观设置选背景图（选择 → 复制进图片目录），以及 OCR 读取已入库的图片。
 */
export function registerImageIPC(): void {
  ipcMain.handle(IPC_CHANNELS.DIALOG_SELECT_IMAGE, async (event) => {
    const result = await showSenderOwnedOpenDialog(event, {
      properties: ["openFile", "multiSelections"],
      filters: [
        { name: "Images", extensions: ["jpg", "png", "gif", "jpeg", "webp"] },
      ],
    });

    if (!result.canceled && result.filePaths.length > 0) {
      lastSelectedImagePaths = new Set(
        result.filePaths.map((filePath) => path.resolve(filePath)),
      );
      return result.filePaths;
    }
    lastSelectedImagePaths = new Set();
    return [];
  });

  // 只接受刚由文件选择器选出的路径，渲染进程不能借此复制任意文件
  ipcMain.handle(
    IPC_CHANNELS.IMAGE_SAVE,
    async (_event, filePaths: string[]) => {
      const imagesDir = getImagesDir();

      await fs.mkdir(imagesDir, { recursive: true });

      const savedImages: string[] = [];

      for (const filePath of filePaths) {
        try {
          const resolvedFilePath = path.resolve(filePath);
          if (!isAllowedSelectedImagePath(resolvedFilePath)) {
            throw new Error(
              "Image path was not selected through the file picker",
            );
          }

          const ext = path.extname(filePath);
          if (!IMAGE_EXTENSIONS.has(ext.toLowerCase())) {
            throw new Error("Unsupported image type");
          }
          const fileName = `${randomUUID()}${ext}`;
          const destPath = path.join(imagesDir, fileName);

          await fs.copyFile(resolvedFilePath, destPath);
          savedImages.push(fileName);
        } catch (error) {
          console.error(`Failed to save image ${filePath}:`, error);
        }
      }

      lastSelectedImagePaths = new Set();

      return savedImages;
    },
  );

  ipcMain.handle(
    IPC_CHANNELS.IMAGE_READ_BASE64,
    async (_event, fileName: string) => {
      const imagesDir = getImagesDir();

      try {
        const imagePath = validateFileName(fileName, imagesDir);
        if (!(await pathExists(imagePath))) {
          return null;
        }
        const buffer = await fs.readFile(imagePath);
        return buffer.toString("base64");
      } catch (error) {
        console.error(`Failed to read image ${fileName}:`, error);
        return null;
      }
    },
  );
}
