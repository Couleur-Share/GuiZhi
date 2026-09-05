import fs from "fs";
import os from "os";
import path from "path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  cleanupFunasrBackups,
  createFunasrBackup,
  discardFunasrBackup,
  markFunasrBackup,
} from "../../../src/main/services/media/funasr-update-backup";

let root: string;
const original = { installedAt: "2026-09-01", funasrVersion: "1.3.29" };
beforeEach(() => {
  root = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-backup-test-"));
});
afterEach(() => {
  vi.restoreAllMocks();
  fs.rmSync(root, { recursive: true, force: true });
});
const create = () => createFunasrBackup(root, original, "1.4.14");
const deadOwner = () =>
  vi.spyOn(process, "kill").mockImplementation(() => {
    throw Object.assign(new Error("gone"), { code: "ESRCH" });
  });

describe("FunASR 备份生命周期", () => {
  it("备份目录出现额外用户文件时保留整个目录", async () => {
    const backup = await create();
    const personal = path.join(backup.directory, "personal-note.txt");
    fs.writeFileSync(personal, "keep me");
    expect(await discardFunasrBackup(backup)).toBeTruthy();
    expect(fs.readFileSync(personal, "utf8")).toBe("keep me");
  });
  it("已有临时记录若是硬链接，不得覆盖外部文件", async () => {
    const backup = await create();
    const personal = path.join(root, "personal-note.txt");
    fs.writeFileSync(personal, "keep me");
    fs.linkSync(personal, path.join(backup.directory, "update.json.tmp"));
    await markFunasrBackup(backup, "ready");
    expect(fs.readFileSync(personal, "utf8")).toBe("keep me");
  });
  it("每次备份有独立标识和原安装记录，不复用旧目录", async () => {
    const a = await create();
    const b = await create();
    expect(a.directory).not.toBe(b.directory);
    expect(
      JSON.parse(
        fs.readFileSync(path.join(a.directory, "update.json"), "utf8"),
      ),
    ).toMatchObject({ original, phase: "copying", targetVersion: "1.4.14" });
  });
  it("仅清理明确完成的备份，进程退出也不推断中断备份可删", async () => {
    const completed = await create();
    await markFunasrBackup(completed, "disposable");
    const active = await create();
    expect(await cleanupFunasrBackups(root)).toHaveLength(1);
    expect(fs.existsSync(completed.directory)).toBe(false);
    expect(fs.existsSync(active.directory)).toBe(true);
    deadOwner();
    expect(await cleanupFunasrBackups(root)).toHaveLength(1);
    expect(fs.existsSync(active.directory)).toBe(true);
  });
  it("复制完成但中断的备份仍保留", async () => {
    const backup = await create();
    await markFunasrBackup(backup, "ready");
    deadOwner();
    await cleanupFunasrBackups(root);
    expect(fs.existsSync(backup.directory)).toBe(true);
  });
  it("进入写入/恢复阶段、无记录、损坏记录、手动 pip 副本均保留", async () => {
    const updating = await create();
    await markFunasrBackup(updating, "updating");
    const recovery = await create();
    await markFunasrBackup(recovery, "recovery-required");
    const damaged = await create();
    fs.writeFileSync(path.join(damaged.directory, "update.json"), "{");
    fs.mkdirSync(path.join(root, "update-backup-legacy"));
    fs.mkdirSync(path.join(root, "pip-repair-backup-manual"));
    deadOwner();
    const warnings = await cleanupFunasrBackups(root);
    expect(warnings).toHaveLength(5);
    expect(fs.readdirSync(root)).toHaveLength(5);
  });
  it("清理失败保留可丢弃标记，下一次可重试", async () => {
    const backup = await create();
    const remove = vi
      .spyOn(fs.promises, "rm")
      .mockImplementationOnce(async () => {
        fs.unlinkSync(path.join(backup.directory, "update.json"));
        throw new Error("EPERM");
      });
    expect(await discardFunasrBackup(backup)).toContain("下次更新会重试");
    expect(
      JSON.parse(
        fs.readFileSync(path.join(backup.directory, "update.json"), "utf8"),
      ).phase,
    ).toBe("disposable");
    remove.mockRestore();
    expect(await cleanupFunasrBackups(root)).toEqual([]);
    expect(fs.existsSync(backup.directory)).toBe(false);
  });
  it("指向外部的备份目录链接不能触发递归删除", async () => {
    const outside = path.join(root, "keep");
    fs.mkdirSync(outside);
    fs.writeFileSync(path.join(outside, "sentinel"), "keep");
    fs.symlinkSync(
      outside,
      path.join(root, "update-backup-ABC123"),
      "junction",
    );
    expect(await cleanupFunasrBackups(root)).toHaveLength(1);
    expect(fs.readFileSync(path.join(outside, "sentinel"), "utf8")).toBe(
      "keep",
    );
  });
  it("同名目录被替换后不删除替换目录中的文件", async () => {
    const backup = await create();
    await markFunasrBackup(backup, "disposable");
    const old = path.join(root, "original");
    fs.renameSync(backup.directory, old);
    fs.mkdirSync(backup.directory);
    fs.copyFileSync(
      path.join(old, "update.json"),
      path.join(backup.directory, "update.json"),
    );
    fs.mkdirSync(path.join(backup.directory, "env"));
    const personal = path.join(backup.directory, "env/personal.txt");
    fs.writeFileSync(personal, "keep");
    expect(await cleanupFunasrBackups(root)).toHaveLength(1);
    expect(fs.readFileSync(personal, "utf8")).toBe("keep");
    expect(await discardFunasrBackup(backup)).toBeTruthy();
  });
  it("备份内部的目录连接保留，外部目标文件不受影响", async () => {
    const backup = await create();
    const outside = path.join(root, "outside");
    fs.mkdirSync(outside);
    fs.writeFileSync(path.join(outside, "sentinel"), "keep");
    fs.mkdirSync(path.join(backup.directory, "env"));
    fs.symlinkSync(
      outside,
      path.join(backup.directory, "env/link"),
      "junction",
    );
    expect(await discardFunasrBackup(backup)).toBeTruthy();
    expect(fs.readFileSync(path.join(outside, "sentinel"), "utf8")).toBe(
      "keep",
    );
    expect(fs.existsSync(backup.directory)).toBe(true);
  });
  it("旧格式即使标记可丢弃也不能自动删除", async () => {
    const backup = await create();
    await markFunasrBackup(backup, "disposable");
    const marker = path.join(backup.directory, "update.json");
    const data = JSON.parse(fs.readFileSync(marker, "utf8"));
    data.schema = 1;
    fs.writeFileSync(marker, JSON.stringify(data));
    expect(await cleanupFunasrBackups(root)).toHaveLength(1);
    expect(fs.existsSync(backup.directory)).toBe(true);
  });
  it("扫描后状态变成需要恢复时不继续删除", async () => {
    const backup = await create();
    await markFunasrBackup(backup, "disposable");
    const marker = path.join(backup.directory, "update.json");
    const read = fs.promises.readFile.bind(fs.promises);
    vi.spyOn(fs.promises, "readFile").mockImplementationOnce(
      async (...args: Parameters<typeof fs.promises.readFile>) => {
        const result = await read(...args);
        const data = JSON.parse(fs.readFileSync(marker, "utf8"));
        data.phase = "recovery-required";
        fs.writeFileSync(marker, JSON.stringify(data));
        return result;
      },
    );
    expect(await cleanupFunasrBackups(root)).toHaveLength(1);
    expect(fs.existsSync(backup.directory)).toBe(true);
    expect(JSON.parse(fs.readFileSync(marker, "utf8")).phase).toBe(
      "recovery-required",
    );
  });
  it("根目录被替换成连接后不读写链接指向的备份", async () => {
    const backup = await create();
    const movedRoot = `${root}-moved`;
    fs.renameSync(root, movedRoot);
    fs.symlinkSync(movedRoot, root, "junction");
    try {
      expect(await discardFunasrBackup(backup)).toBeTruthy();
      expect(
        fs.existsSync(
          path.join(movedRoot, path.basename(backup.directory), "update.json"),
        ),
      ).toBe(true);
    } finally {
      fs.unlinkSync(root);
      fs.renameSync(movedRoot, root);
    }
  });
});
