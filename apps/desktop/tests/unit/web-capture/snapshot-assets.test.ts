import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { configureRuntimePaths, getImagesDir } from "../../../src/main/runtime-paths";
import { downloadToTempFile } from "../../../src/main/services/import/safe-fetch";
import { collectSnapshotAssets, leasedSnapshotAssets, releaseSnapshotAssets } from "../../../src/main/services/web-capture/snapshot-assets";
vi.mock("../../../src/main/services/import/safe-fetch",()=>({downloadToTempFile:vi.fn()}));
let directory:string;
const png=Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl6nWQAAAAASUVORK5CYII=","base64");
beforeEach(async()=>{
  directory=await fs.mkdtemp(path.join(os.tmpdir(),"guizhi-snapshot-assets-"));
  configureRuntimePaths({userDataPath:directory});
  vi.mocked(downloadToTempFile).mockImplementation(async()=>{
    const dir=await fs.mkdtemp(path.join(directory,"download-")),filePath=path.join(dir,"image");
    await fs.writeFile(filePath,png);return {dir,filePath};
  });
});
afterEach(async()=>{await fs.rm(directory,{recursive:true,force:true});vi.clearAllMocks();});
describe("离线资源发布与失败边界",()=>{
  it("SVG 经清理转为 PNG，外链和脚本不会进入发布文件",async()=>{
    vi.mocked(downloadToTempFile).mockImplementationOnce(async()=>{
      const dir=await fs.mkdtemp(path.join(directory,"svg-")),filePath=path.join(dir,"image");
      await fs.writeFile(filePath,'<svg xmlns="http://www.w3.org/2000/svg" width="30" height="30"><script>alert(1)</script><rect width="20" height="20" fill="red"/><image href="https://evil.test/a"/></svg>');
      return {dir,filePath};
    });
    const result=await collectSnapshotAssets(["https://public.test/a.svg"],new AbortController().signal);
    expect(result.failures).toEqual([]);expect(result.assets[0].fileName).toMatch(/\.png$/);
    const data=await fs.readFile(path.join(getImagesDir(),result.assets[0].fileName));
    expect(data.subarray(1,4).toString()).toBe("PNG");expect(data.includes(Buffer.from("evil.test"))).toBe(false);
    releaseSnapshotAssets(result.assets);
  });
  it("不同 URL 的相同图片原子去重，任务租约分别释放",async()=>{
    const result=await collectSnapshotAssets(["https://public.test/a","https://public.test/b"],new AbortController().signal);
    expect(result.failures).toEqual([]);expect(result.assets).toHaveLength(2);
    expect(await fs.readdir(getImagesDir())).toEqual([result.assets[0].fileName]);
    expect(await fs.readFile(path.join(getImagesDir(),result.assets[0].fileName))).toEqual(png);
    releaseSnapshotAssets([result.assets[0]]);expect(leasedSnapshotAssets().has(result.assets[0].fileName)).toBe(true);
    releaseSnapshotAssets([result.assets[1]]);expect(leasedSnapshotAssets().has(result.assets[0].fileName)).toBe(false);
  });
  it("单图失败保留其原因与已保存图片，不远程回退",async()=>{
    vi.mocked(downloadToTempFile).mockRejectedValueOnce(new Error("重定向地址被 SSRF 校验拒绝"));
    const result=await collectSnapshotAssets(["https://public.test/a","https://public.test/b"],new AbortController().signal);
    expect(result.failures).toEqual([{url:"https://public.test/a",reason:"重定向地址被 SSRF 校验拒绝"}]);
    expect(result.assets).toHaveLength(1);releaseSnapshotAssets(result.assets);
  });
  it("已取消任务不启动下载，并记账未处理资源",async()=>{
    const controller=new AbortController();controller.abort();
    const result=await collectSnapshotAssets(["https://public.test/a"],controller.signal);
    expect(downloadToTempFile).not.toHaveBeenCalled();expect(result.failures).toHaveLength(1);expect(result.assets).toEqual([]);
  });
});
