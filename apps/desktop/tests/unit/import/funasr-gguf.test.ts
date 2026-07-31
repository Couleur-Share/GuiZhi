import fs from "fs";
import os from "os";
import path from "path";
import { afterEach, describe, expect, it } from "vitest";
import {
  findGgufSensevoiceCli,
  GGUF_CLI_NAME,
  getFunasrPaths,
  isFunasrGgufInstalled,
  isFunasrPythonInstalled,
  resolveFunasrEngineFlavor,
} from "../../../src/main/services/media/funasr-paths";
import {
  buildGgufCliArgs,
  extractMultipartFile,
  extractTranscriptFromCliStdout,
  parseMultipartBoundary,
} from "../../../src/main/services/media/funasr-gguf-shim";

describe("funasr-paths GGUF", () => {
  const dirs: string[] = [];

  afterEach(() => {
    for (const dir of dirs.splice(0)) {
      fs.rmSync(dir, { recursive: true, force: true });
    }
  });

  function tempRoot(): string {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-funasr-"));
    dirs.push(dir);
    return dir;
  }

  it("findGgufSensevoiceCli 能在一层子目录里找到 CLI", () => {
    const root = tempRoot();
    const nested = path.join(root, "bin");
    fs.mkdirSync(nested, { recursive: true });
    const cli = path.join(nested, GGUF_CLI_NAME);
    fs.writeFileSync(cli, "");
    expect(findGgufSensevoiceCli(root)).toBe(cli);
  });

  it("isFunasrGgufInstalled 要求 CLI + 两个 GGUF", () => {
    const root = tempRoot();
    const paths = getFunasrPaths(root);
    expect(isFunasrGgufInstalled(paths)).toBe(false);
    expect(isFunasrPythonInstalled(paths)).toBe(false);
    expect(resolveFunasrEngineFlavor(paths)).toBeNull();

    fs.mkdirSync(paths.ggufRuntimeDir, { recursive: true });
    fs.mkdirSync(paths.ggufModelsDir, { recursive: true });
    fs.writeFileSync(paths.sensevoiceCli, "");
    fs.writeFileSync(paths.sensevoiceGguf, "m");
    fs.writeFileSync(paths.vadGguf, "v");

    expect(isFunasrGgufInstalled(paths)).toBe(true);
    expect(resolveFunasrEngineFlavor(paths)).toBe("gguf");
  });
});

describe("funasr-gguf-shim multipart / CLI", () => {
  it("解析 boundary 与 file 字段", () => {
    const boundary = "----GuiZhiBoundary";
    const contentType = `multipart/form-data; boundary=${boundary}`;
    expect(parseMultipartBoundary(contentType)).toBe(boundary);

    const payload = Buffer.from("hello-audio");
    const body = Buffer.concat([
      Buffer.from(
        `--${boundary}\r\n` +
          `Content-Disposition: form-data; name="file"; filename="a.wav"\r\n` +
          `Content-Type: audio/wav\r\n` +
          `\r\n`,
      ),
      payload,
      Buffer.from(`\r\n--${boundary}--\r\n`),
    ]);

    const file = extractMultipartFile(contentType, body);
    expect(file.filename).toBe("a.wav");
    expect(file.data.equals(payload)).toBe(true);
  });

  it("拼 CLI 参数并清洗 stdout", () => {
    expect(
      buildGgufCliArgs(
        {
          cliPath: "/opt/llama-funasr-sensevoice",
          modelPath: "/m/sensevoice-small-q8.gguf",
          vadPath: "/m/fsmn-vad.gguf",
        },
        "/tmp/a.wav",
      ),
    ).toEqual([
      "/opt/llama-funasr-sensevoice",
      "-m",
      "/m/sensevoice-small-q8.gguf",
      "--vad",
      "/m/fsmn-vad.gguf",
      "-a",
      "/tmp/a.wav",
    ]);
    expect(extractTranscriptFromCliStdout("  你好世界\r\n")).toBe("你好世界");
  });
});
