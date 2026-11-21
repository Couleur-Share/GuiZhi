import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { SnapshotToc } from "../components/library/SnapshotToc";
import { useSnapshotReader } from "../components/library/use-snapshot-reader";
import type { ReaderBridge } from "./bridge";
import { restoreLocalImageUrl } from "./media-urls";
import { inlineLocalImages, localImageRefs, type InlinedSnapshot } from "./snapshot-images";
import type { HostView, SnapshotWidth } from "./protocol";

/**
 * 网页快照视图：渲染层 WebSnapshotPane 的 original 分支搬进阅读岛——滚动区里一个沙箱 iframe（srcDoc）加目录浮层。
 *
 * 安全（台账 A-018）：iframe 里是不可信的网页内容，沙箱、来源校验、外链放行规则与 WebSnapshotPane 逐字一致：
 * - `sandbox="allow-scripts"` 且不带 allow-same-origin，iframe 是不透明来源，`referrerPolicy="no-referrer"`；
 * - 只认 `event.source === 本 iframe 的 contentWindow && event.origin === "null" && event.data.id === instanceId` 的消息；
 * - 外链只放行 http/https 且不带用户名口令，再交宿主（宿主另有一道 SafeLink 校验），页面自己不导航、不 window.open。
 * - 文档里的本地图片由本页（可信的主框架）读出来内联成 data: 地址（snapshot-images.ts）：不透明来源的 iframe 够不到岛的自有地址。
 *
 * 与 WebSnapshotPane 的差别只在「能力从哪来」：查找词、宽度、目录开合由宿主给（原生工具栏与查找条在宿主一侧），
 * 命中数、外链、点图、Esc 与 Ctrl+F 经桥交回宿主；目录是否常驻窄轨（wideRail）与目录开合经 snapshotState 上报，
 * 让原生工具栏知道要不要显示「目录」钮以及它的 aria-expanded。
 */

/** WebSnapshotPane 里的坐标上限：来自 iframe 的数值一律夹在这个范围内。 */
const MAX_COORDINATE = 2_000_000;
/** 宿主一侧 link 事件的长度上限（viva/reader MaxLinkBytes）：更长的地址宿主会整条丢弃，页面不必发。 */
const MAX_LINK_LENGTH = 8 << 10;

interface Props {
  bridge: ReaderBridge;
  state: HostView;
  find: { query: string; index: number };
  ui: { tocOpen: boolean; snapshotWidth: SnapshotWidth };
  setUi: (patch: { tocOpen?: boolean }) => void;
}

/** 严格校验 iframe 请求打开的外链：返回规整后的地址，不合规返回 null（与 WebSnapshotPane 同一条规则）。 */
export function safeSnapshotLink(value: string): string | null {
  try {
    const url = new URL(value);
    if (["http:", "https:"].includes(url.protocol) && !url.username && !url.password) {
      return url.href.length <= MAX_LINK_LENGTH ? url.href : null;
    }
  } catch {
    /* 无效外链不执行 */
  }
  return null;
}

export function SnapshotIslandView({ bridge, state, find, ui, setUi }: Props) {
  const { t } = useTranslation();
  const view = state.view as Extract<HostView["view"], { kind: "snapshot" }>;
  const { instanceId, assets } = view;
  const html = view.document;
  const [height, setHeight] = useState(400);
  // 本地图片内联：没有本地图片的文档同步就绪（行为与之前一样）；有的先读图，读完才把文档交给 iframe（期间 iframe 不可见、不上报 rendered）。
  const refs = useMemo(() => localImageRefs(html), [html]);
  const [inlined, setInlined] = useState<(InlinedSnapshot & { source: string }) | null>(null);
  useEffect(() => {
    if (refs.length === 0) return;
    const abort = new AbortController();
    inlineLocalImages(html, refs, { signal: abort.signal })
      .then((result) => setInlined({ ...result, source: html }))
      .catch(() => {
        /* 被中止（换了文档或卸载）：丢弃 */
      });
    return () => abort.abort();
  }, [html, refs]);
  const prepared: (InlinedSnapshot & { source?: string }) | null =
    refs.length === 0 ? { doc: html, originals: new Map() } : inlined?.source === html ? inlined : null;
  const srcDoc = prepared?.doc ?? "";
  const preparedRef = useRef(prepared);
  preparedRef.current = prepared;
  const frame = useRef<HTMLIFrameElement>(null);
  const scroll = useRef<HTMLDivElement>(null);
  // 阅读位置按「条目 + 版本」记在页面的 localStorage 里（与 WebSnapshotPane 同一份记忆）；目录开合由宿主持有，不用 hook 自带的那份。
  const reader = useSnapshotReader(frame, scroll, instanceId, `${state.itemId}:${view.versionId || "current"}`, true);

  const seqRef = useRef(state.seq);
  seqRef.current = state.seq;
  const tocOpenRef = useRef(ui.tocOpen);
  tocOpenRef.current = ui.tocOpen;

  // 页内查找：把宿主给的查找词转交 iframe；request 递增，过期的回包（find-result）不算数。
  const findRequest = useRef(0);
  const sendFind = useCallback(() => {
    const target = frame.current?.contentWindow;
    if (!target) return;
    target.postMessage(
      {
        id: instanceId,
        type: "find",
        value: { query: find.query.slice(0, 1000), index: Math.max(0, find.index), request: ++findRequest.current },
      },
      "*",
    );
  }, [instanceId, find.query, find.index]);
  useEffect(sendFind, [sendFind]);

  // rendered：每个视图序号报一次——iframe 加载完成时，或同一份文档在新序号下（宽度之外的更新）直接确认。
  const loadedDocument = useRef<string | null>(null);
  const onLoad = useCallback(() => {
    if (!preparedRef.current) return; // 文档还在读图：这次加载是空白占位，不算渲染完成
    loadedDocument.current = html;
    sendFind();
    bridge.send({ t: "rendered", seq: seqRef.current });
  }, [bridge, html, sendFind]);
  useEffect(() => {
    if (loadedDocument.current === html) bridge.send({ t: "rendered", seq: state.seq });
  }, [bridge, html, state.seq]);

  useEffect(() => {
    const receive = (event: MessageEvent) => {
      if (
        event.source !== frame.current?.contentWindow ||
        event.origin !== "null" ||
        event.data?.id !== instanceId
      ) {
        return;
      }
      const { type, value } = event.data;
      const seq = seqRef.current;
      if (type === "find-open" && value === null) {
        // Ctrl+F：交宿主打开查找条（与窗口收到 Ctrl+F 同一件事）。
        bridge.send({ t: "key", seq, key: "f", ctrl: true, alt: false, shift: false });
      }
      if (
        type === "find-result" &&
        value?.request === findRequest.current &&
        Number.isSafeInteger(value.count) &&
        value.count >= 0 &&
        value.count <= 10000
      ) {
        bridge.send({ t: "matches", seq, count: value.count });
        if (typeof value.top === "number" && Number.isFinite(value.top)) {
          scroll.current?.scrollTo({ top: Math.max(0, Math.min(value.top - 60, MAX_COORDINATE)) });
        }
      }
      if (type === "escape" && value === null) {
        // Esc：退出专注阅读（宿主一侧的快捷键处理）。
        bridge.send({ t: "key", seq, key: "escape", ctrl: false, alt: false, shift: false });
      }
      if (type === "height" && typeof value === "number" && Number.isFinite(value)) {
        setHeight(Math.max(100, Math.min(value, MAX_COORDINATE)));
      }
      if (type === "anchor" && typeof value === "number" && Number.isFinite(value)) {
        scroll.current?.scrollTo({ top: Math.max(0, Math.min(value, MAX_COORDINATE)) });
      }
      if (type === "link" && typeof value === "string") {
        const href = safeSnapshotLink(value);
        if (href) bridge.send({ t: "link", seq, url: href });
      }
      if (type === "image" && typeof value === "string") {
        // 宿主把文档里的 local-image:// 改写成了岛里的相对路径、本页又把读到的图片内联成 data: 地址：桥接脚本回报的是 data: 地址（或没读到的图片的相对地址），换回清单里的写法再找。
        const original = preparedRef.current?.originals.get(value) ?? restoreLocalImageUrl(value);
        const index = assets.indexOf(original);
        if (index >= 0) {
          bridge.send({ t: "lightbox", seq, images: assets.map((src) => ({ src })), index });
        }
      }
    };
    window.addEventListener("message", receive);
    return () => window.removeEventListener("message", receive);
  }, [bridge, instanceId, assets]);

  // 版面状态上报：窄轨是否常驻（窗口宽度决定）变化时，以及每个新序号（宿主丢弃序号过期的事件）。目录开合由页面发起的变化另行上报。
  useEffect(() => {
    bridge.send({ t: "snapshotState", seq: state.seq, wideRail: reader.wideRail, open: tocOpenRef.current });
  }, [bridge, state.seq, reader.wideRail]);

  const changeToc = useCallback(
    (open: boolean) => {
      setUi({ tocOpen: open });
      bridge.send({ t: "snapshotState", seq: seqRef.current, wideRail: reader.wideRail, open });
    },
    [bridge, reader.wideRail, setUi],
  );

  return (
    <div className="min-h-0 flex-1" data-article-reader {...snapshotAttrs(state)}>
      <div className="relative h-full min-h-0">
        <div ref={scroll} className="h-full min-h-0 overflow-auto bg-muted/30 p-3">
          <iframe
            data-article-instance={instanceId}
            data-article-target={JSON.stringify({
              itemId: state.itemId,
              view: "snapshot",
              versionId: view.versionId || undefined,
            })}
            ref={frame}
            onLoad={onLoad}
            aria-label={t("library.snapshotFrame", "微信公众号原文快照")}
            sandbox="allow-scripts"
            referrerPolicy="no-referrer"
            srcDoc={srcDoc}
            style={{
              width:
                ui.snapshotWidth === "original"
                  ? 390
                  : ui.snapshotWidth === "wide"
                    ? "100%"
                    : `min(100%, ${reader.comfortableWidth}px)`,
              maxWidth: "100%",
              height,
              visibility: prepared ? undefined : "hidden",
            }}
            className="mx-auto block border-0 bg-white"
          />
        </div>
        <SnapshotToc
          headings={reader.headings}
          active={reader.active}
          progress={reader.progress}
          wide={reader.wideRail}
          open={ui.tocOpen}
          onOpen={changeToc}
          onJump={reader.jump}
          onTop={reader.top}
        />
      </div>
    </div>
  );
}

/** 渲染层 ContentPanel 在网页快照原文上写的 data-article-*（问答据此认出选中的文字属于快照）。 */
function snapshotAttrs(state: HostView): { "data-article-editing": "false"; "data-article-target": string } {
  return {
    "data-article-editing": "false",
    "data-article-target": JSON.stringify({ itemId: state.itemId, view: "snapshot", sourceKind: "body" }),
  };
}
