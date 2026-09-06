"""版本化 stdio；仅本文件预置的页面操作，网页请求必须由主进程提供响应。"""
import asyncio
import json
import os
import sys
import time

PROTOCOL = 1
MAX_FRAME = 16 * 1024 * 1024
protocol_out = sys.stdout
protocol_out.reconfigure(encoding="utf-8", errors="strict")
sys.stdout = sys.stderr
os.environ["LITELLM_LOCAL_MODEL_COST_MAP"] = "True"
os.environ["HF_HUB_OFFLINE"] = "1"


def forbid_python_network(event, args):
    if event in ("socket.connect", "socket.getaddrinfo", "urllib.Request"):
        raise RuntimeError("Python 正文处理不得直连网络，请经主进程请求服务")


from playwright.async_api import async_playwright
from extract import extract
from broker import BrowserBroker

pending = {}
tasks = {}
browser = None
broker = None


def emit(message):
    value = json.dumps(dict(v=PROTOCOL, **message), ensure_ascii=False)
    if len(value.encode()) > MAX_FRAME:
        raise ValueError("采集消息超过限制")
    protocol_out.write(value + "\n")
    protocol_out.flush()


async def capture(message):
    task_id = message["taskId"]
    context = None
    context_id = None
    outgoing = None
    try:
        context, context_id = await broker.new_context(browser, task_id)
        page = await context.new_page()
        await asyncio.wait_for(broker.ready[context_id].wait(), 30)
        page.on("popup", lambda popup: asyncio.create_task(popup.close()))
        page.on("download", lambda download: asyncio.create_task(download.cancel()))
        emit(dict(type="stage", taskId=task_id, stage="fetching"))
        response = await page.goto(message["url"], wait_until="domcontentloaded", timeout=45000)
        if response and any(kind in response.headers.get("content-type", "") for kind in ("application/pdf", "application/json", "application/octet-stream")):
            raise ValueError("入口返回非网页内容")
        # 给异步正文一个有界窗口；不依赖永不空闲的广告网络。
        await page.wait_for_timeout(1200)
        for _ in range(3):
            await page.evaluate("window.scrollBy(0, Math.min(window.innerHeight, 1000))")
            await page.wait_for_timeout(200)
        html = await page.content()
        if len(html.encode()) > 10 * 1024 * 1024:
            raise ValueError("渲染后 HTML 超过 10 MiB")
        final_url = page.url
        links = await page.locator("a[href]").evaluate_all("nodes => nodes.slice(0, 2000).map(a => a.href)")
        emit(dict(type="stage", taskId=task_id, stage="extracting"))
        result = await asyncio.to_thread(extract, html, final_url, response.status if response else 0)
        outgoing = dict(type="result", taskId=task_id, result=dict(result, taskId=task_id,
                  entryUrl=message["url"], finalUrl=final_url, links=links,
                  capturedAt=int(time.time() * 1000), engineVersion="crawl4ai/0.9.3"))
    except asyncio.CancelledError:
        outgoing = dict(type="error", taskId=task_id, error="采集已取消", code="canceled")
    except Exception as error:
        # 不输出含 Cookie、代理口令、查询参数的库异常。
        outgoing = dict(type="error", taskId=task_id, error=str(error) if isinstance(error, ValueError) else "网页采集失败（" + type(error).__name__ + "）", code="timeout" if "Timeout" in type(error).__name__ else "incomplete" if isinstance(error, ValueError) else "network")
        failed_url, status = broker.documents.get(task_id, (message["url"], 0))
        if status >= 400:
            result = await asyncio.to_thread(extract, "", failed_url, status)
            outgoing = dict(type="result", taskId=task_id, result=dict(result, taskId=task_id,
                entryUrl=message["url"], finalUrl=failed_url, links=[], capturedAt=int(time.time()*1000), engineVersion="crawl4ai/0.9.3"))
    finally:
        if context:
            await context.close()
        if context_id:
            broker.remove_context(context_id)
        tasks.pop(task_id, None)
        if outgoing:
            emit(outgoing)


async def main():
    global browser, broker
    # Windows 事件循环先建立自己的 socketpair；之后正文处理禁止新网络连接。
    sys.addaudithook(forbid_python_network)
    async with async_playwright() as playwright:
        browser = await playwright.chromium.connect_over_cdp(sys.argv[1], timeout=45000)
        broker = BrowserBroker(await browser.new_browser_cdp_session(), emit, pending)
        await broker.start()
        emit(dict(type="ready"))
        while True:
            raw = await asyncio.to_thread(sys.stdin.buffer.readline, MAX_FRAME + 1)
            if not raw:
                break
            if len(raw) > MAX_FRAME:
                raise ValueError("协议消息过大")
            message = json.loads(raw)
            if message.get("v") != PROTOCOL:
                raise ValueError("协议版本不匹配")
            kind = message.get("type")
            if kind == "capture":
                if len(tasks) >= 2 or message["taskId"] in tasks:
                    emit(dict(type="error", taskId=message["taskId"], error="采集并发超过限制", code="network"))
                else:
                    tasks[message["taskId"]] = asyncio.create_task(capture(message))
            elif kind == "network-result" and message["id"] in pending:
                future, _ = pending[message["id"]]
                if not future.done():
                    future.set_result(message["response"])
            elif kind == "cancel" and message["taskId"] in tasks:
                tasks[message["taskId"]].cancel()
            elif kind == "shutdown":
                break
        for task in list(tasks.values()):
            task.cancel()
        await asyncio.gather(*list(tasks.values()), return_exceptions=True)
        # CDP 连接的 browser.close 可能只断开客户端；显式关闭本组件拥有的浏览器。
        try:
            session = await browser.new_browser_cdp_session()
            await session.send("Browser.close")
        except Exception:
            pass
        await browser.close()


asyncio.run(main())
