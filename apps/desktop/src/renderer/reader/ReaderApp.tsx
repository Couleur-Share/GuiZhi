import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useTranslation } from "react-i18next";
import i18n from "i18next";
import {
  filterForumReplies,
  parseForumReplySection,
  resolveReplyTargetFloor,
  type ForumReplyEntry,
} from "@guizhi/shared/utils/forum-note";
import {
  ForumDiscussionView,
  type ForumDiscussionHandle,
} from "../components/library/ForumDiscussionView";
import { ForumFloorCatalog } from "../components/library/ForumFloorCatalog";
import { ImageGallery } from "../components/library/ImageGallery";
import { MarkdownPreview } from "../components/library/MarkdownPreview";
import { SnapshotToc } from "../components/library/SnapshotToc";
import { useMarkFindNavigation } from "../components/library/use-mark-find";
import type { ReaderBridge } from "./bridge";
import { setLightboxOpener } from "./lightbox-shim";
import {
  READER_PROTOCOL,
  type HostView,
  type ReaderLang,
  type ReaderTheme,
} from "./protocol";
import { applyTheme } from "./theme";

/**
 * 阅读岛的页面：宿主发来「要显示什么」（HostView）、查找词（find）、外观（theme），页面把它画出来，
 * 并把查找命中数、滚动位置、点击的链接与图片、复制的代码等事件交还宿主（协议见 protocol.ts）。
 *
 * 画法与渲染层 ContentPanel 里 data-article-reader 内的各个分支一一对应，用的是同一批组件与同一份样式表；
 * 外层包装 div 的类名与层级也照抄（高度链、绝对定位的目录浮层要靠它们）。
 */

/** 滚动位置的上报防抖（渲染层 useDebouncedScrollSave 的 200 ms）。 */
const SCROLL_REPORT_MS = 200;

/** 需要转交宿主的窗口级快捷键（渲染层在 window 上监听它们，岛有键盘焦点时宿主收不到）。 */
const FORWARDED_KEYS = new Set(["f", "s", "z", "arrowleft", "arrowright"]);

interface FindState {
  query: string;
  index: number;
}

interface UiState {
  catalogOpen: boolean;
  tocOpen: boolean;
}

interface ViewProps {
  bridge: ReaderBridge;
  state: HostView;
  find: FindState;
  ui: UiState;
  setUi: (patch: Partial<UiState>) => void;
  /** 本地改查找状态（楼层跳转需要立刻清掉过滤词，不能等宿主绕一圈）。 */
  setFind: (next: FindState) => void;
}

export function ReaderApp({ bridge }: { bridge: ReaderBridge }) {
  const [look, setLook] = useState<{
    theme: ReaderTheme;
    lang: ReaderLang;
  } | null>(null);
  const [state, setState] = useState<HostView | null>(null);
  const [find, setFind] = useState<FindState>({ query: "", index: 0 });
  const [ui, setUiState] = useState<UiState>({
    catalogOpen: false,
    tocOpen: false,
  });
  const seqRef = useRef(0);
  seqRef.current = state?.seq ?? 0;
  // 等宿主回执的复制：id → writeText 返回的 Promise 的两端（台账 B-069）。
  const copyIdRef = useRef(0);
  const pendingCopies = useRef(
    new Map<number, { resolve: () => void; reject: (reason: Error) => void }>(),
  );

  const setUi = useCallback((patch: Partial<UiState>) => {
    setUiState((previous) => ({ ...previous, ...patch }));
  }, []);

  useEffect(
    () =>
      bridge.listen((message) => {
        switch (message.t) {
          case "init":
          case "theme":
            setLook({ theme: message.theme, lang: message.lang });
            break;
          case "view":
            setState(message);
            setUiState({
              catalogOpen: message.catalogOpen,
              tocOpen: message.tocOpen,
            });
            break;
          case "find":
            setFind({ query: message.query, index: message.index });
            break;
          case "ui":
            setUiState({
              catalogOpen: message.catalogOpen,
              tocOpen: message.tocOpen,
            });
            break;
          case "copyResult": {
            const entry = pendingCopies.current.get(message.id);
            if (!entry) break;
            pendingCopies.current.delete(message.id);
            // 渲染层 writeText 失败时 Promise 拒绝（CodeBlock 的 catch 静默）：宿主写入失败同样拒绝，勾不出现。
            if (message.ok) entry.resolve();
            else entry.reject(new Error("clipboard write failed"));
            break;
          }
        }
      }),
    [bridge],
  );

  // 订阅装好之后再报 ready：宿主收到它才开始发状态。
  useEffect(() => {
    bridge.send({ t: "ready", v: READER_PROTOCOL });
  }, [bridge]);

  useLayoutEffect(() => {
    if (!look) return;
    applyTheme(document.documentElement, look.theme, look.lang);
    void i18n.changeLanguage(look.lang);
  }, [look]);

  // 页面级的接管：链接、右键菜单、剪贴板、快捷键、查看器。
  useEffect(() => {
    const onLink = (event: MouseEvent): void => {
      if (event.type === "auxclick" && event.button !== 1) return;
      const anchor = (event.target as Element | null)?.closest?.("a[href]");
      if (!(anchor instanceof HTMLAnchorElement)) return;
      // 页内锚点留给浏览器自己滚动；其余一律不让页面导航，交宿主校验后用系统浏览器打开。
      if (anchor.getAttribute("href")?.startsWith("#")) return;
      // 捕获阶段拦下：组件自己的点击处理（调渲染层的 window.api 打开链接）在岛里没有宿主能力，不能再跑。
      event.preventDefault();
      event.stopPropagation();
      bridge.send({ t: "link", seq: seqRef.current, url: anchor.href });
    };
    const onContextMenu = (event: Event): void => event.preventDefault();
    const onKeyDown = (event: KeyboardEvent): void => {
      const key = event.key.toLowerCase();
      if ((!event.ctrlKey && !event.altKey) || !FORWARDED_KEYS.has(key)) return;
      event.preventDefault();
      bridge.send({
        t: "key",
        seq: seqRef.current,
        key,
        ctrl: event.ctrlKey,
        alt: event.altKey,
        shift: event.shiftKey,
      });
    };
    document.addEventListener("click", onLink, true);
    document.addEventListener("auxclick", onLink, true);
    document.addEventListener("contextmenu", onContextMenu);
    document.addEventListener("keydown", onKeyDown, true);
    // 页面里的 navigator.clipboard.writeText 换成「交宿主复制」：不依赖页面焦点与用户手势，写入失败由宿主如实提示。
    // 宿主写完回 copyResult，Promise 据此成功或拒绝，所以代码块复制钮的「已复制」勾只在写入成功之后出现（与渲染层一致，台账 B-069）。
    // 代码块的复制按钮等照常调用 navigator.clipboard.writeText。
    const previousClipboard = Object.getOwnPropertyDescriptor(navigator, "clipboard");
    const copies = pendingCopies.current;
    Object.defineProperty(navigator, "clipboard", {
      value: {
        writeText: (text: string): Promise<void> =>
          new Promise<void>((resolve, reject) => {
            const id = ++copyIdRef.current;
            copies.set(id, { resolve, reject });
            bridge.send({ t: "copy", seq: seqRef.current, id, text });
          }),
      },
      configurable: true,
    });
    setLightboxOpener((images, index) =>
      bridge.send({
        t: "lightbox",
        seq: seqRef.current,
        images: images.map(({ src, alt }) => ({ src, alt })),
        index,
      }),
    );
    return () => {
      document.removeEventListener("click", onLink, true);
      document.removeEventListener("auxclick", onLink, true);
      document.removeEventListener("contextmenu", onContextMenu);
      document.removeEventListener("keydown", onKeyDown, true);
      setLightboxOpener(null);
      copies.clear();
      if (previousClipboard) Object.defineProperty(navigator, "clipboard", previousClipboard);
      else Reflect.deleteProperty(navigator, "clipboard");
    };
  }, [bridge]);

  if (!state) return <div className="h-full" />;
  const props: ViewProps = { bridge, state, find, ui, setUi, setFind };
  // 渲染层按条目与标签换分支（key={item.id}）：换了条目或标签就重建，滚动回到顶部；同一条的内容更新只换内容。
  const key = `${state.itemId}:${state.tab}:${state.view.kind}`;
  return (
    <div className="relative flex h-full min-h-0 flex-col overflow-hidden app-wallpaper-panel">
      <ViewBody key={key} {...props} />
    </div>
  );
}

/**
 * 渲染层 ContentPanel 在 data-article-reader 上写的两个属性（问答据此找到选中的文字属于哪一块正文，article-reader-target.ts）：
 * 岛里不编辑，editing 恒为 "false"；target 的取值同 ContentPanel（图片标签算正文；源码类型取原文的来源，其余是正文）。
 */
function articleAttrs(state: HostView): { "data-article-editing": "false"; "data-article-target": string } {
  const sourceKind = state.view.kind === "original" ? state.view.sourceKind : "body";
  const view = state.tab === "images" ? "body" : state.tab;
  return { "data-article-editing": "false", "data-article-target": JSON.stringify({ itemId: state.itemId, view, sourceKind }) };
}

function ViewBody(props: ViewProps) {
  const { view } = props.state;
  switch (view.kind) {
    case "none":
      return <div className="min-h-0 flex-1" data-article-reader {...articleAttrs(props.state)} />;
    case "original":
      return <OriginalView {...props} />;
    case "markdown":
      return <MarkdownView {...props} />;
    case "replies":
      return <RepliesView {...props} />;
    case "gallery":
      return <GalleryView {...props} />;
  }
}

/**
 * 滚动位置：恢复（打开这条时一次）与上报（200 ms 防抖，换内容或卸载时把没发出的补发）。
 * getTop 返回主滚动元素的 scrollTop，没有可滚的元素返回 null。
 */
function useScroll(
  props: ViewProps,
  getTop: () => number | null,
  setTop: (top: number) => void,
) {
  const { bridge, state } = props;
  const seqRef = useRef(state.seq);
  seqRef.current = state.seq;
  const getTopRef = useRef(getTop);
  getTopRef.current = getTop;
  const pending = useRef<{
    timer: ReturnType<typeof setTimeout>;
    top: number;
  } | null>(null);

  const flush = useCallback(() => {
    const entry = pending.current;
    if (!entry) return;
    clearTimeout(entry.timer);
    pending.current = null;
    bridge.send({ t: "scroll", seq: seqRef.current, top: entry.top });
  }, [bridge]);

  const restore = state.restoreTop;
  const setTopRef = useRef(setTop);
  setTopRef.current = setTop;
  useLayoutEffect(() => {
    if (restore == null) return;
    // 等子树挂好再应用（渲染层 requestAnimationFrame(apply)）。
    const frame = requestAnimationFrame(() => setTopRef.current(restore));
    return () => cancelAnimationFrame(frame);
    // 只在打开这条时恢复一次，之后的内容更新不再拉回去。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => flush, [flush]);

  return useCallback(() => {
    const top = getTopRef.current();
    if (top == null) return;
    if (pending.current) clearTimeout(pending.current.timer);
    pending.current = { top, timer: setTimeout(flush, SCROLL_REPORT_MS) };
  }, [flush]);
}

/** 命中数与当前命中的上报；seq 取最新的（内容更新不改回调身份，避免子组件的 effect 重跑）。 */
function useMatchReporter(props: ViewProps) {
  const { bridge, state } = props;
  const seqRef = useRef(state.seq);
  seqRef.current = state.seq;
  return useCallback(
    (count: number) =>
      bridge.send({ t: "matches", seq: seqRef.current, count }),
    [bridge],
  );
}

function useRendered(props: ViewProps) {
  const { bridge, state } = props;
  useEffect(() => {
    bridge.send({ t: "rendered", seq: state.seq });
  }, [bridge, state.seq]);
}

/** 主题阅读的「原文」：ReconstructionReader 里 showPage 为假时的画法，外加目录浮层与标题位置跟踪。 */
function OriginalView(props: ViewProps) {
  const { bridge, state, find, ui, setUi } = props;
  const view = state.view as Extract<HostView["view"], { kind: "original" }>;
  const scrollRef = useRef<HTMLDivElement>(null);
  const onScroll = useScroll(
    props,
    () => scrollRef.current?.scrollTop ?? null,
    (top) => {
      if (scrollRef.current) scrollRef.current.scrollTop = top;
    },
  );
  useRendered(props);
  const reportMatches = useMatchReporter(props);
  useMarkFindNavigation({
    containerRef: scrollRef,
    query: find.query,
    activeIndex: find.index,
    onMatchCountChange: reportMatches,
    contentKey: `${state.itemId}:${state.tab}:${view.content.length}`,
  });

  const [headings, setHeadings] = useState<
    { index: number; text: string; top: number }[]
  >([]);
  const [position, setPosition] = useState({
    active: null as number | null,
    progress: 0,
  });
  // 与 ReconstructionReader 同一段：量出 h1–h3 的位置，随滚动算当前章节与阅读进度。
  useEffect(() => {
    const element = scrollRef.current;
    if (!element) return;
    const list = [...element.querySelectorAll("h1,h2,h3")].map(
      (heading, index) => ({
        index,
        text: heading.textContent ?? "",
        top:
          heading.getBoundingClientRect().top -
          element.getBoundingClientRect().top +
          element.scrollTop,
      }),
    );
    setHeadings(list);
    const rememberPosition = () =>
      setPosition({
        active:
          list.filter((heading) => heading.top <= element.scrollTop + 80).at(-1)
            ?.index ?? null,
        progress:
          element.scrollHeight > element.clientHeight
            ? Math.min(
                100,
                Math.round(
                  (element.scrollTop /
                    (element.scrollHeight - element.clientHeight)) *
                    100,
                ),
              )
            : 100,
      });
    rememberPosition();
    element.addEventListener("scroll", rememberPosition, { passive: true });
    return () => element.removeEventListener("scroll", rememberPosition);
  }, [view.content]);

  const changeToc = useCallback(
    (open: boolean) => {
      setUi({ tocOpen: open });
      bridge.send({ t: "toc", seq: state.seq, open });
    },
    [bridge, setUi, state.seq],
  );

  return (
    <div className="min-h-0 flex-1" onScrollCapture={onScroll} data-article-reader {...articleAttrs(state)}>
      <div
        className="relative flex h-full min-h-0 flex-col"
        data-testid="reconstruction-reader"
      >
        <div className="relative min-h-0 flex-1">
          <div
            className="relative flex h-full min-h-0 flex-col"
            data-testid="themed-reading-pane"
          >
            <div className="min-h-0 flex-1">
              <MarkdownPreview
                ref={scrollRef}
                content={view.content}
                highlightQuery={find.query}
              />
            </div>
          </div>
          <SnapshotToc
            headings={headings}
            active={position.active}
            progress={position.progress}
            wide={false}
            open={ui.tocOpen}
            onOpen={changeToc}
            onJump={(index) =>
              scrollRef.current?.scrollTo({ top: headings[index]?.top ?? 0 })
            }
            onTop={() => scrollRef.current?.scrollTo({ top: 0 })}
          />
        </div>
      </div>
    </div>
  );
}

/** 纯 Markdown 预览：MarkdownTabPane 的画法（图片条目的识别文字等）。 */
function MarkdownView(props: ViewProps) {
  const { state, find } = props;
  const view = state.view as Extract<HostView["view"], { kind: "markdown" }>;
  const scrollRef = useRef<HTMLDivElement>(null);
  const onScroll = useScroll(
    props,
    () => scrollRef.current?.scrollTop ?? null,
    (top) => {
      if (scrollRef.current) scrollRef.current.scrollTop = top;
    },
  );
  useRendered(props);
  const reportMatches = useMatchReporter(props);
  useMarkFindNavigation({
    containerRef: scrollRef,
    query: find.query,
    activeIndex: find.index,
    onMatchCountChange: reportMatches,
    contentKey: `${state.itemId}:${state.tab}:${view.content.length}`,
  });
  return (
    <div className="min-h-0 flex-1" onScrollCapture={onScroll} data-article-reader {...articleAttrs(state)}>
      <div className="h-full">
        <div className="h-full" aria-hidden={false}>
          <MarkdownPreview
            ref={scrollRef}
            content={view.content}
            highlightQuery={find.query}
          />
        </div>
      </div>
    </div>
  );
}

/** 与 ContentPanel 里的 filterIncludesFloor 同一个判断：这一楼在当前过滤词下是否可见。 */
function filterIncludesFloor(
  replies: ForumReplyEntry[],
  query: string,
  floor: number,
): boolean {
  const reply = replies.find((entry) => entry.floor === floor);
  if (!reply) return false;
  return filterForumReplies([reply], query).length > 0;
}

/** 论坛讨论：楼层卡片 + 楼层目录（ContentPanel 里 replies 分支的画法与它的跳楼逻辑）。 */
function RepliesView(props: ViewProps) {
  const { bridge, state, find, ui, setUi, setFind } = props;
  const { t } = useTranslation();
  const view = state.view as Extract<HostView["view"], { kind: "replies" }>;
  const replies = useMemo(
    () => parseForumReplySection(view.content),
    [view.content],
  );
  const discussionRef = useRef<ForumDiscussionHandle>(null);
  const [activeFloor, setActiveFloor] = useState<number | null>(null);
  const onScroll = useScroll(
    props,
    () => discussionRef.current?.getScrollElement()?.scrollTop ?? null,
    (top) => {
      const element = discussionRef.current?.getScrollElement();
      if (element) element.scrollTop = top;
    },
  );
  useRendered(props);
  const reportMatches = useMatchReporter(props);

  const selectFloor = useCallback(
    (floor: number) => {
      const visible =
        !find.query.trim() || filterIncludesFloor(replies, find.query, floor);
      if (!visible) {
        // 过滤词挡住了目标楼：清掉它（本地立刻清，宿主随后同步查找条里的文字）。
        setFind({ query: "", index: 0 });
        bridge.send({ t: "findReset", seq: state.seq });
      }
      setActiveFloor(floor);
      requestAnimationFrame(() => {
        discussionRef.current?.scrollToFloor(floor);
      });
    },
    [bridge, find.query, replies, setFind, state.seq],
  );

  const replyToClick = useCallback(
    (replyTo: NonNullable<ForumReplyEntry["replyTo"]>) => {
      const target = resolveReplyTargetFloor(replies, replyTo);
      if (target == null) {
        bridge.send({
          t: "toast",
          seq: state.seq,
          kind: "warning",
          text: t("library.forumReplyJumpMiss", "该楼未入库或无法定位"),
        });
        return;
      }
      selectFloor(target);
    },
    [bridge, replies, selectFloor, state.seq, t],
  );

  return (
    <div className="min-h-0 flex-1" onScrollCapture={onScroll} data-article-reader {...articleAttrs(state)}>
      <div className="h-full">
        <div className="h-full" aria-hidden={false}>
          <ForumDiscussionView
            ref={discussionRef}
            content={view.content}
            query={find.query}
            activeIndex={find.index}
            findNavEnabled
            onMatchCountChange={reportMatches}
            onReplyToClick={replyToClick}
            catalog={
              <ForumFloorCatalog
                replies={replies}
                open={ui.catalogOpen}
                onOpenChange={(open) => {
                  setUi({ catalogOpen: open });
                  bridge.send({ t: "catalog", seq: state.seq, open });
                }}
                activeFloor={activeFloor}
                onSelectFloor={selectFloor}
              />
            }
          />
        </div>
      </div>
    </div>
  );
}

/** 图片条目的配图画廊。 */
function GalleryView(props: ViewProps) {
  const view = props.state.view as Extract<HostView["view"], { kind: "gallery" }>;
  useRendered(props);
  return (
    <div className="min-h-0 flex-1" data-article-reader {...articleAttrs(props.state)}>
      <ImageGallery content={view.content} />
    </div>
  );
}
