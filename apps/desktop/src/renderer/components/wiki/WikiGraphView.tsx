import { lazy, Suspense, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { WikiGraphNode, WikiPageKind } from "@guizhi/shared/types";
import { useWikiStore } from "../../stores/wiki.store";
import { useSettingsStore } from "../../stores/settings.store";
import { Spinner } from "../ui/Spinner";

// react-force-graph-2d 体积较大（含 d3-force），仅进入图谱视图时加载
const ForceGraph2D = lazy(() => import("react-force-graph-2d"));

type GraphNode = WikiGraphNode & { x?: number; y?: number };

const KIND_COLORS: Record<WikiPageKind, string> = {
  topic: "#3b82f6",
  entity: "#10b981",
  concept: "#f59e0b",
};

const NODE_LABEL_MAX_LENGTH = 14;
const LABEL_VISIBLE_SCALE = 1.4;

function truncateLabel(title: string): string {
  return title.length > NODE_LABEL_MAX_LENGTH
    ? `${title.slice(0, NODE_LABEL_MAX_LENGTH)}…`
    : title;
}

function GraphLegend() {
  const { t } = useTranslation();
  const items: Array<{ kind: WikiPageKind; labelKey: string; fallback: string }> = [
    { kind: "topic", labelKey: "wiki.kindTopic", fallback: "主题" },
    { kind: "entity", labelKey: "wiki.kindEntity", fallback: "实体" },
    { kind: "concept", labelKey: "wiki.kindConcept", fallback: "概念" },
  ];
  return (
    <div className="pointer-events-none absolute bottom-3 left-3 flex items-center gap-3 rounded-lg border border-border/60 bg-background/70 px-3 py-1.5 backdrop-blur">
      {items.map((item) => (
        <span
          key={item.kind}
          className="inline-flex items-center gap-1.5 text-[11px] text-muted-foreground"
        >
          <span
            className="h-2.5 w-2.5 rounded-full"
            style={{ backgroundColor: KIND_COLORS[item.kind] }}
            aria-hidden="true"
          />
          {t(item.labelKey, item.fallback)}
        </span>
      ))}
    </div>
  );
}

/**
 * Wiki 关系图谱：页面为节点（按类型着色），页间链接为边。
 * 点击节点跳转该页面并切回目录视图；缩放到一定级别显示标题标签。
 */
export function WikiGraphView() {
  const { t } = useTranslation();
  const graph = useWikiStore((state) => state.graph);
  const selectedPageId = useWikiStore((state) => state.selectedPageId);
  const selectPage = useWikiStore((state) => state.selectPage);
  const setViewMode = useWikiStore((state) => state.setViewMode);
  const loadGraph = useWikiStore((state) => state.loadGraph);
  const isDarkMode = useSettingsStore((state) => state.isDarkMode);

  const containerRef = useRef<HTMLDivElement>(null);
  const [size, setSize] = useState({ width: 0, height: 0 });

  // setViewMode("graph") 已经拉过一次，挂载时不必再拉
  useEffect(() => {
    if (!graph) {
      void loadGraph();
    }
    // 只在挂载时判断一次，graph 变化不该重新触发
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loadGraph]);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) {
      return;
    }
    const observer = new ResizeObserver((entries) => {
      const rect = entries[0]?.contentRect;
      if (rect) {
        setSize({
          width: Math.round(rect.width),
          height: Math.round(rect.height),
        });
      }
    });
    observer.observe(container);
    return () => observer.disconnect();
  }, []);

  // force-graph 会往对象上写模拟坐标，传入拷贝避免污染 store
  const graphData = useMemo(
    () => ({
      nodes: (graph?.nodes ?? []).map((node) => ({ ...node })),
      links: (graph?.links ?? []).map((link) => ({ ...link })),
    }),
    [graph],
  );

  const labelColor = isDarkMode
    ? "rgba(226, 232, 240, 0.85)"
    : "rgba(51, 65, 85, 0.85)";

  if (!graph) {
    return (
      <div className="flex flex-1 items-center justify-center">
        <Spinner size="sm" tone="muted" />
      </div>
    );
  }

  if (graph.nodes.length === 0) {
    return (
      <div className="flex flex-1 items-center justify-center px-6 text-center text-sm text-muted-foreground">
        {t("wiki.graphEmpty", "还没有页面关系，编译 Wiki 后自动生成")}
      </div>
    );
  }

  return (
    <div ref={containerRef} className="relative min-h-0 flex-1 overflow-hidden">
      {graph.totalNodes > graph.nodes.length ? (
        <div className="pointer-events-none absolute left-3 top-3 z-10 rounded-lg border border-border/70 bg-background/85 px-2.5 py-1 text-[11px] text-muted-foreground backdrop-blur">
          {t(
            "wiki.graphTruncated",
            "按连接度显示 {{shown}} / {{total}} 个页面",
            { shown: graph.nodes.length, total: graph.totalNodes },
          )}
        </div>
      ) : null}
      {size.width > 0 && size.height > 0 ? (
        <Suspense
          fallback={
            <div className="flex h-full items-center justify-center">
              <Spinner size="sm" tone="muted" />
            </div>
          }
        >
          <ForceGraph2D
            width={size.width}
            height={size.height}
            graphData={graphData}
            nodeLabel={(node) => (node as GraphNode).title}
            linkColor={() => "rgba(148, 163, 184, 0.35)"}
            linkDirectionalArrowLength={3}
            linkDirectionalArrowRelPos={1}
            cooldownTicks={150}
            onNodeClick={(node) => {
              const id = String((node as GraphNode).id);
              void selectPage(id);
              setViewMode("catalog");
            }}
            nodeCanvasObject={(node, ctx, globalScale) => {
              const graphNode = node as GraphNode & { x: number; y: number };
              const isSelected = graphNode.id === selectedPageId;
              const radius = isSelected ? 6 : 4.5;

              ctx.beginPath();
              ctx.arc(graphNode.x, graphNode.y, radius, 0, 2 * Math.PI);
              ctx.fillStyle = KIND_COLORS[graphNode.kind] ?? KIND_COLORS.topic;
              ctx.fill();
              if (isSelected) {
                ctx.lineWidth = 1.5;
                ctx.strokeStyle = labelColor;
                ctx.stroke();
              }

              if (globalScale >= LABEL_VISIBLE_SCALE || isSelected) {
                const fontSize = 12 / globalScale;
                ctx.font = `${fontSize}px sans-serif`;
                ctx.textAlign = "center";
                ctx.textBaseline = "top";
                ctx.fillStyle = labelColor;
                ctx.fillText(
                  truncateLabel(graphNode.title),
                  graphNode.x,
                  graphNode.y + radius + 2,
                );
              }
            }}
            nodePointerAreaPaint={(node, color, ctx) => {
              const graphNode = node as GraphNode & { x: number; y: number };
              ctx.beginPath();
              ctx.arc(graphNode.x, graphNode.y, 8, 0, 2 * Math.PI);
              ctx.fillStyle = color;
              ctx.fill();
            }}
          />
        </Suspense>
      ) : null}
      <GraphLegend />
    </div>
  );
}
