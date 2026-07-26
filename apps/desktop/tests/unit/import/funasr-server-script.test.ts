import fs from "fs";
import os from "os";
import path from "path";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  FUNASR_SERVER_SCRIPT,
  ensureFunasrServerScript,
} from "../../../src/main/services/media/funasr-server-script";

function makeTempDir(): string {
  return fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-funasr-script-"));
}

const tempDirs: string[] = [];

function tempScriptPath(): string {
  const dir = makeTempDir();
  tempDirs.push(dir);
  return path.join(dir, "nested", "server.py");
}

afterEach(() => {
  vi.restoreAllMocks();
  while (tempDirs.length > 0) {
    fs.rmSync(tempDirs.pop() as string, { recursive: true, force: true });
  }
});

describe("FUNASR_SERVER_SCRIPT", () => {
  it("打开了 funasr-server 漏掉的两个开关", () => {
    // 这两行是整个自建脚本存在的理由，被删掉就退回无标点 + 合成时间轴
    expect(FUNASR_SERVER_SCRIPT).toContain('"use_itn": True');
    expect(FUNASR_SERVER_SCRIPT).toContain('"sentence_timestamp": True');
  });

  it("模型实例不与 model 表单字段同名", () => {
    // 端点按 OpenAI 协议必须收一个叫 model 的表单字段，闭包里的 AutoModel
    // 若也叫 model 会被它遮蔽，运行时报 'str' object has no attribute 'generate'
    expect(FUNASR_SERVER_SCRIPT).toContain("asr_model.generate(**kwargs)");
    expect(FUNASR_SERVER_SCRIPT).not.toContain("    model = AutoModel(");
  });

  it("保留调用方依赖的两个 OpenAI 端点", () => {
    // /v1/models 是健康探测的判据，改路径会让服务永远等不到就绪
    expect(FUNASR_SERVER_SCRIPT).toContain('@app.get("/v1/models")');
    expect(FUNASR_SERVER_SCRIPT).toContain(
      '@app.post("/v1/audio/transcriptions")',
    );
  });

  it("富文本标签正则在落盘后是合法的 Python 原始字符串", () => {
    // TS 模板字符串里 \\| 才能写出 Python 侧的 \|，写漏一层就是坏正则
    expect(FUNASR_SERVER_SCRIPT).toContain(
      'RICH_TAG = re.compile(r"<\\|([^|]*)\\|>")',
    );
  });
});

describe("ensureFunasrServerScript", () => {
  it("目录不存在时一并建出来并写入脚本", () => {
    const scriptPath = tempScriptPath();
    ensureFunasrServerScript(scriptPath);

    expect(fs.readFileSync(scriptPath, "utf8")).toBe(FUNASR_SERVER_SCRIPT);
  });

  it("内容已是最新时不再写盘", () => {
    const scriptPath = tempScriptPath();
    ensureFunasrServerScript(scriptPath);

    const writeSpy = vi.spyOn(fs, "writeFileSync");
    ensureFunasrServerScript(scriptPath);

    expect(writeSpy).not.toHaveBeenCalled();
  });

  it("内容过期（应用升级）时覆盖为新版", () => {
    const scriptPath = tempScriptPath();
    fs.mkdirSync(path.dirname(scriptPath), { recursive: true });
    fs.writeFileSync(scriptPath, "# 旧版脚本\n", "utf8");

    ensureFunasrServerScript(scriptPath);

    expect(fs.readFileSync(scriptPath, "utf8")).toBe(FUNASR_SERVER_SCRIPT);
  });
});
