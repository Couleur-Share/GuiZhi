/**
 * 本地转写服务的 Python 脚本（内嵌为常量，服务启动前落盘）。
 *
 * 不用 funasr 自带的 funasr-server CLI，是因为它调 AutoModel 时不传
 * use_itn 与 sentence_timestamp（funasr 1.3.29 的
 * funasr/bin/_server_app.py::_process_fallback 只组了 input/batch_size/
 * language），而它的 HTTP 端点也没有透传这两个参数的口子。后果实测过：
 * SenseVoice 走 woitn 分支输出整段无标点文本，verbose_json 的 segments
 * 退化成按字符数均分总时长的合成值（等距，对不上语音）。
 *
 * 脚本内嵌而非作为资源文件打包，是为了让已安装的用户升级应用后自动拿到
 * 新版脚本，不必重装引擎。
 */
import fs from "fs";
import path from "path";

export const FUNASR_SERVER_SCRIPT = `"""归知本地转写服务：OpenAI 兼容的 /v1/audio/transcriptions。

由 funasr-server-script.ts 在服务启动前写入，请勿手工编辑。

与 funasr 自带的 funasr-server 的唯一实质差别是打开了 use_itn 与
sentence_timestamp：前者出标点与逆文本归一化，后者让 AutoModel 生成
sentence_info（VAD 真实句级时间轴）。两者都不额外耗时。
"""
import argparse
import os
import re
import tempfile

import soundfile as sf
import uvicorn
from fastapi import FastAPI, File, Form, HTTPException, UploadFile
from fastapi.responses import JSONResponse, PlainTextResponse
from funasr import AutoModel

CHECKPOINT = "iic/SenseVoiceSmall"
VAD_CHECKPOINT = "fsmn-vad"
# VAD 单段时长上限（毫秒），与 funasr-server 的取值保持一致
VAD_MAX_SEGMENT_MS = 30000

RICH_TAG = re.compile(r"<\\|([^|]*)\\|>")
LANGUAGE_TAGS = ("zh", "en", "yue", "ja", "ko", "nospeech")


def strip_tags(text):
    """剥掉 SenseVoice 的富文本标签（语种 / 情感 / 音频事件）。"""
    return RICH_TAG.sub("", text or "").strip()


def detect_language(raw_text):
    for tag in RICH_TAG.findall(raw_text or ""):
        if tag in LANGUAGE_TAGS:
            return tag
    return "unknown"


def build_segments(result):
    """把 sentence_info 转成 OpenAI 的 segments（毫秒转秒）。"""
    segments = []
    for index, sentence in enumerate(result.get("sentence_info") or []):
        text = strip_tags(sentence.get("text", ""))
        if not text:
            continue
        segments.append(
            {
                "id": index,
                "start": round(sentence.get("start", 0) / 1000, 3),
                "end": round(sentence.get("end", 0) / 1000, 3),
                "text": text,
                "words": [],
            }
        )
    return segments


def probe_duration(audio_path):
    try:
        return round(float(sf.info(audio_path).duration), 3)
    except Exception:
        return 0.0


def create_app(model_name, device):
    app = FastAPI(title="GuiZhi Transcription Server")

    # 在 uvicorn 起监听之前把模型加载完：调用方以 /v1/models 可达作为
    # 「服务就绪」的判据，端口先开会让首次转写撞上模型加载。
    print("[guizhi-asr] loading %s on %s" % (CHECKPOINT, device), flush=True)
    # 不叫 model：转写端点的表单字段按 OpenAI 协议就叫 model，同名会遮蔽它
    asr_model = AutoModel(
        model=CHECKPOINT,
        vad_model=VAD_CHECKPOINT,
        vad_kwargs={"max_single_segment_time": VAD_MAX_SEGMENT_MS},
        device=device,
        disable_update=True,
    )
    print("[guizhi-asr] model ready", flush=True)

    @app.get("/v1/models")
    async def list_models():
        return JSONResponse(
            {"object": "list", "data": [{"id": model_name, "object": "model"}]}
        )

    @app.post("/v1/audio/transcriptions")
    async def transcriptions(
        file: UploadFile = File(...),
        model: str = Form(default=""),
        language: str = Form(default=""),
        response_format: str = Form(default="json"),
    ):
        if model and model != model_name:
            raise HTTPException(
                400, "Unknown model '%s', expected '%s'" % (model, model_name)
            )

        suffix = os.path.splitext(file.filename or "")[1] or ".wav"
        with tempfile.NamedTemporaryFile(delete=False, suffix=suffix) as tmp:
            tmp.write(await file.read())
            tmp_path = tmp.name
        try:
            kwargs = {
                "input": tmp_path,
                "batch_size": 1,
                "use_itn": True,
                "sentence_timestamp": True,
            }
            if language:
                kwargs["language"] = language
            results = asr_model.generate(**kwargs)
            duration = probe_duration(tmp_path)
        finally:
            os.unlink(tmp_path)

        # 静音样本（连通性测试用）会走到这里：results 为空或 text 为空串
        result = results[0] if results else {}
        raw_text = result.get("text", "")
        text = strip_tags(raw_text)

        if response_format == "text":
            return PlainTextResponse(text)
        if response_format == "verbose_json":
            return JSONResponse(
                {
                    "task": "transcribe",
                    "language": detect_language(raw_text),
                    "duration": duration,
                    "text": text,
                    "segments": build_segments(result),
                }
            )
        return JSONResponse({"text": text})

    return app


def main():
    parser = argparse.ArgumentParser(description="GuiZhi local transcription server")
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=8620)
    parser.add_argument("--model", default="sensevoice")
    parser.add_argument("--device", default="cpu")
    args = parser.parse_args()
    uvicorn.run(
        create_app(args.model, args.device),
        host=args.host,
        port=args.port,
        log_level="info",
    )


if __name__ == "__main__":
    main()
`;

/** 落盘服务脚本；内容一致时跳过写入，避免每次启动都触碰磁盘 */
export function ensureFunasrServerScript(scriptPath: string): void {
  try {
    if (fs.readFileSync(scriptPath, "utf8") === FUNASR_SERVER_SCRIPT) {
      return;
    }
  } catch {
    // 文件不存在或读取失败，照常写入
  }
  fs.mkdirSync(path.dirname(scriptPath), { recursive: true });
  fs.writeFileSync(scriptPath, FUNASR_SERVER_SCRIPT, "utf8");
}
