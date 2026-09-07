import { expect, it, vi } from "vitest";
import fs from "node:fs";
import path from "node:path";
import Database from "@guizhi/db/adapter";
import { configureRuntimePaths } from "../../src/main/runtime-paths";
import { BackupRepository } from "../../src/main/services/backup-repository";
import { prepareRepositoryRestore } from "../../src/main/services/backup-repository-restore";
import { readSnapshot } from "../../src/main/services/web-capture/snapshot-service";
import { exportKnowledgeToMarkdown } from "../../src/main/services/export-markdown";
vi.mock("electron",()=>({dialog:{}}));

// 仅使用已结束的 pnpm shot 独立实例，不接触真实用户数据库。
it.skipIf(!process.env.GUIZHI_WECHAT_SNAPSHOT_PROFILE)("真实图文的备份恢复及 Markdown 离线导出",async()=>{
  const root=path.resolve(process.env.GUIZHI_WECHAT_SNAPSHOT_PROFILE!);
  expect(path.basename(root)).toMatch(/^guizhi-shot-/);
  const paths={databasePath:path.join(root,"data/knowledge.db"),imagesDir:path.join(root,"data/assets/images"),videosDir:path.join(root,"data/assets/videos"),configDir:path.join(root,"config")};
  const db=new Database(paths.databasePath);
  const repository=new BackupRepository({...paths,repositoryDir:path.join(root,"snapshot-acceptance-repository")},{backend:"isolated-test",isAvailable:()=>true,isSecure:()=>true,wrap:key=>key,unwrap:key=>key});
  let restored:Database|undefined;
  try {
    repository.initialize("isolated acceptance password");
    const saved=repository.createSnapshot({db,appVersion:"acceptance",request:{}});
    expect(saved.success).toBe(true);
    expect(saved.snapshot.summary.assetCount).toBe(20);
    const prepared=prepareRepositoryRestore({repository,snapshotId:saved.snapshot.fileName,liveDb:db,targets:paths});
    configureRuntimePaths({userDataPath:prepared.stageDir});
    restored=new Database(prepared.databasePath);
    const row=restored.get("SELECT id FROM knowledge_items LIMIT 1") as {id:string};
    const view=await readSnapshot(restored,row.id);
    expect(view.error).toBeUndefined();expect(view.edited).toBe(true);
    expect(view.version.snapshot.assets).toHaveLength(20);
    expect(view.version.snapshot.html).toContain("text-align:center");
    const destination=path.join(root,"markdown-acceptance");fs.mkdirSync(destination,{recursive:true});
    const exported=exportKnowledgeToMarkdown(restored,destination);
    expect(exported).toEqual({count:1,assetCount:19});
    const file=fs.readdirSync(destination).find(name=>name.endsWith(".md"));
    const markdown=fs.readFileSync(path.join(destination,file!),"utf8");
    expect(markdown).toContain("人工追加：验收保护标记");
    expect(markdown).not.toContain("local-image://");
    expect(markdown).not.toContain("<iframe");
    expect(fs.readdirSync(path.join(destination,"assets"))).toHaveLength(19);
    fs.writeFileSync(path.join(root,"restore-acceptance.json"),JSON.stringify({passed:true,assets:20,markdown:exported,edited:view.edited},null,2));
  } finally {restored?.close();db.close();}
});
