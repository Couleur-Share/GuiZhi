"""版本化 stdio 正文提取；由主进程串行发送 HTML，Python 不得自行联网。"""
import json
import os
import sys

MAX_FRAME = 16 * 1024 * 1024
protocol_out = sys.stdout
protocol_out.reconfigure(encoding="utf-8", errors="strict")
sys.stdout = sys.stderr
os.environ["LITELLM_LOCAL_MODEL_COST_MAP"] = "True"
os.environ["HF_HUB_OFFLINE"] = "1"


def forbid_network(event, args):
    if event in ("socket.connect", "socket.getaddrinfo", "urllib.Request"):
        raise RuntimeError("正文提取进程不得直连网络")


def emit(message):
    line = json.dumps(dict(v=1, **message), ensure_ascii=False) + "\n"
    if len(line.encode("utf-8")) > MAX_FRAME:
        raise ValueError("正文输出超过限制")
    protocol_out.write(line)
    protocol_out.flush()


sys.addaudithook(forbid_network)
from extract import extract

emit(dict(type="ready"))
while True:
    raw = sys.stdin.buffer.readline(MAX_FRAME + 1)
    if not raw:
        break
    if len(raw) > MAX_FRAME:
        raise ValueError("正文输入超过限制")
    message = {}
    try:
        message = json.loads(raw)
        if message.get("v") != 1:
            raise ValueError("协议版本不匹配")
        result = extract(message["html"], message["url"], message["status"])
        emit(dict(id=message["id"], result=result))
    except Exception as error:
        emit(dict(id=message.get("id", ""), error=type(error).__name__))
