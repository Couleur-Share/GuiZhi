"""CDP Fetch 在每一跳暂停请求，避免 Playwright route 跳过 HTTP 重定向。"""
import asyncio
import base64
import json
import uuid


class BrowserBroker:
    def __init__(self, session, emit, pending):
        self.session, self.emit, self.pending = session, emit, pending
        self.contexts = {}
        self.targets = {}
        self.replies = {}
        self.ready = {}
        self.sequence = 0
        self.parents = {}
        self.lock = asyncio.Lock()
        self.documents = {}

    async def start(self):
        self.session.on("Target.attachedToTarget", lambda event: asyncio.create_task(self.attached(event)))
        self.session.on("Target.receivedMessageFromTarget", self.received)
        self.session.on("Target.targetCreated", lambda event: asyncio.create_task(self.target_created(event)))
        await self.session.send("Target.setDiscoverTargets", dict(discover=True))

    async def target_created(self, event):
        if event["targetInfo"]["type"] == "page":
            try:
                await self.session.send("Target.attachToTarget", dict(targetId=event["targetInfo"]["targetId"], flatten=False))
            except Exception:
                pass

    async def command(self, session_id, method, params=None):
        self.sequence += 1
        key = (session_id, self.sequence)
        future = asyncio.get_running_loop().create_future()
        self.replies[key] = future
        try:
            envelope = dict(sessionId=session_id, message=json.dumps(dict(id=key[1], method=method, params=params or {})))
            if session_id in self.parents:
                await self.command(self.parents[session_id], "Target.sendMessageToTarget", envelope)
            else:
                await self.session.send("Target.sendMessageToTarget", envelope)
            return await asyncio.wait_for(future, 30)
        finally:
            self.replies.pop(key, None)

    def received(self, event):
        session_id = event["sessionId"]
        message = json.loads(event["message"])
        if "id" in message:
            future = self.replies.get((session_id, message["id"]))
            if future and not future.done():
                if "error" in message:
                    future.set_exception(RuntimeError("浏览器请求拦截命令失败"))
                else:
                    future.set_result(message.get("result", {}))
        elif message.get("method") == "Fetch.requestPaused":
            asyncio.create_task(self.request(session_id, message["params"]))
        elif message.get("method") == "Target.attachedToTarget":
            self.parents[message["params"]["sessionId"]] = session_id
            asyncio.create_task(self.attached(message["params"]))
        elif message.get("method") == "Target.receivedMessageFromTarget":
            self.received(message["params"])

    async def attached(self, event):
        session_id, info = event["sessionId"], event["targetInfo"]
        parent_info = self.targets.get(self.parents.get(session_id), {})
        if not info.get("browserContextId") and parent_info.get("browserContextId"):
            info["browserContextId"] = parent_info["browserContextId"]
        self.targets[session_id] = info
        try:
            await self.command(session_id, "Fetch.enable", dict(patterns=[dict(urlPattern="*", requestStage="Request")]))
            await self.command(session_id, "Network.enable")
            await self.command(session_id, "Network.setBlockedURLs", dict(urls=["ws://*", "wss://*", "file://*", "ftp://*"]))
            await self.command(session_id, "Target.setAutoAttach", dict(autoAttach=True, waitForDebuggerOnStart=True, flatten=False))
            await self.command(session_id, "Runtime.runIfWaitingForDebugger")
            context_id = info.get("browserContextId")
            if context_id in self.ready:
                self.ready[context_id].set()
        except Exception:
            # 未安装拦截就不能恢复导航；上层初始化超时会关闭该上下文。
            pass

    async def new_context(self, browser, task_id):
        async with self.lock:
            before = set((await self.session.send("Target.getBrowserContexts"))["browserContextIds"])
            context = await browser.new_context(ignore_https_errors=False, service_workers="block", accept_downloads=False)
            after = set((await self.session.send("Target.getBrowserContexts"))["browserContextIds"])
            created = after - before
            if len(created) != 1:
                await context.close()
                raise RuntimeError("无法确认采集上下文归属")
            context_id = created.pop()
            self.contexts[context_id] = task_id
            self.ready[context_id] = asyncio.Event()
            return context, context_id

    async def request(self, session_id, event):
        info = self.targets.get(session_id, {})
        task_id = self.contexts.get(info.get("browserContextId"))
        request = event["request"]
        request_id = str(uuid.uuid4())
        future = asyncio.get_running_loop().create_future()
        self.pending[request_id] = (future, task_id)
        try:
            if not task_id or not request["url"].startswith(("http://", "https://")):
                raise RuntimeError("未授权的浏览器请求")
            self.emit(dict(type="network", id=request_id, taskId=task_id,
                request=dict(url=request["url"], method=request["method"], headers=request["headers"],
                    body=base64.b64encode(request["postData"].encode()).decode() if "postData" in request else None),
                navigation=event.get("resourceType") == "Document",
                mainFrame=info.get("type") == "page" and event.get("frameId") == info.get("targetId")))
            response = await asyncio.wait_for(future, 30)
            if response.get("error"):
                raise RuntimeError("主进程拒绝请求")
            if event.get("resourceType") == "Document" and info.get("type") == "page" and event.get("frameId") == info.get("targetId"):
                self.documents[task_id] = (request["url"], response["status"])
            headers = []
            for key, value in response["headers"].items():
                for part in value.split("\n"):
                    headers.append(dict(name=key, value=part))
            await self.command(session_id, "Fetch.fulfillRequest", dict(requestId=event["requestId"],
                responseCode=response["status"], responseHeaders=headers, body=response["body"]))
        except Exception:
            try:
                await self.command(session_id, "Fetch.failRequest", dict(requestId=event["requestId"], errorReason="BlockedByClient"))
            except Exception:
                pass
        finally:
            self.pending.pop(request_id, None)

    def remove_context(self, context_id):
        task_id = self.contexts.pop(context_id, None)
        self.documents.pop(task_id, None)
        self.ready.pop(context_id, None)
        for key, info in list(self.targets.items()):
            if info.get("browserContextId") == context_id:
                self.targets.pop(key, None)
