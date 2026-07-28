import { useCallback, useMemo, useState } from "react";

/**
 * 列表视图（全宽表格）的列定义与用户偏好持久化。
 * 参考 PromptHub v0.5.9 的 useTableConfig：只把「是否显示」和「列宽」交给用户，
 * 其余属性（最小/最大宽度、吸附侧、是否可调）永远以代码里的默认定义为准，
 * 避免旧版本写入的脏配置把表格搞坏。
 */

export const LIBRARY_COLUMN_IDS = [
  "checkbox",
  "title",
  "snippet",
  "tags",
  "type",
  "source",
  "collection",
  "status",
  "createdAt",
  "updatedAt",
  "actions",
] as const;

export type LibraryColumnId = (typeof LIBRARY_COLUMN_IDS)[number];

export interface LibraryColumn {
  id: LibraryColumnId;
  labelKey: string;
  fallback: string;
  visible: boolean;
  width: number;
  minWidth: number;
  maxWidth?: number;
  resizable: boolean;
  /** 横向滚动时吸附在表格左/右边缘 */
  sticky?: "left" | "right";
  centered?: boolean;
}

export const DEFAULT_LIBRARY_COLUMNS: LibraryColumn[] = [
  {
    id: "checkbox",
    labelKey: "common.select",
    fallback: "选择",
    visible: true,
    width: 44,
    minWidth: 44,
    resizable: false,
    sticky: "left",
  },
  {
    id: "title",
    labelKey: "library.columnTitle",
    fallback: "标题",
    visible: true,
    width: 230,
    minWidth: 150,
    maxWidth: 480,
    resizable: true,
  },
  {
    id: "snippet",
    labelKey: "library.columnSnippet",
    fallback: "摘要",
    visible: true,
    width: 220,
    minWidth: 120,
    maxWidth: 640,
    resizable: true,
  },
  {
    id: "tags",
    labelKey: "library.columnTags",
    fallback: "标签",
    visible: true,
    width: 150,
    minWidth: 90,
    maxWidth: 320,
    resizable: true,
  },
  {
    id: "type",
    labelKey: "library.columnType",
    fallback: "类型",
    visible: true,
    width: 84,
    minWidth: 72,
    maxWidth: 160,
    resizable: true,
    centered: true,
  },
  {
    id: "source",
    labelKey: "library.columnSource",
    fallback: "来源",
    visible: true,
    // 108 而不是 96：多了平台 logo 那 14px，96 会让「哔哩哔哩」默认就被截断
    width: 108,
    minWidth: 80,
    maxWidth: 180,
    resizable: true,
    centered: true,
  },
  {
    id: "collection",
    labelKey: "library.columnCollection",
    fallback: "知识库",
    visible: false,
    width: 140,
    minWidth: 100,
    maxWidth: 280,
    resizable: true,
  },
  {
    id: "status",
    labelKey: "library.columnStatus",
    fallback: "状态",
    visible: false,
    width: 96,
    minWidth: 84,
    maxWidth: 180,
    resizable: true,
    centered: true,
  },
  {
    id: "createdAt",
    labelKey: "library.columnCreatedAt",
    fallback: "创建时间",
    visible: false,
    width: 116,
    minWidth: 100,
    maxWidth: 220,
    resizable: true,
  },
  {
    id: "updatedAt",
    labelKey: "library.columnUpdatedAt",
    fallback: "更新时间",
    visible: true,
    width: 116,
    minWidth: 100,
    maxWidth: 220,
    resizable: true,
  },
  {
    id: "actions",
    labelKey: "library.columnActions",
    fallback: "操作",
    visible: true,
    width: 148,
    minWidth: 148,
    resizable: false,
    sticky: "right",
  },
];

const STORAGE_KEY = "guizhi-library-table-config";

export function clampColumnWidth(column: LibraryColumn, width: unknown): number {
  if (typeof width !== "number" || !Number.isFinite(width)) {
    return column.width;
  }
  const upper = column.maxWidth ?? Number.POSITIVE_INFINITY;
  return Math.min(Math.max(width, column.minWidth), upper);
}

/** 把持久化片段合并进默认列定义；未知列 id 直接丢弃 */
export function mergeStoredColumns(stored: unknown): LibraryColumn[] {
  const saved = (stored as { columns?: unknown } | null)?.columns;
  if (!Array.isArray(saved)) {
    return DEFAULT_LIBRARY_COLUMNS;
  }
  return DEFAULT_LIBRARY_COLUMNS.map((column) => {
    const match = saved.find(
      (candidate): candidate is { visible?: unknown; width?: unknown } =>
        typeof candidate === "object" &&
        candidate !== null &&
        (candidate as { id?: unknown }).id === column.id,
    );
    if (!match) {
      return column;
    }
    return {
      ...column,
      visible:
        typeof match.visible === "boolean" ? match.visible : column.visible,
      width: clampColumnWidth(column, match.width),
    };
  });
}

function readStoredColumns(): LibraryColumn[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    return mergeStoredColumns(raw ? JSON.parse(raw) : null);
  } catch (error) {
    console.warn("读取表格列配置失败，改用默认列:", error);
    return DEFAULT_LIBRARY_COLUMNS;
  }
}

function writeStoredColumns(columns: LibraryColumn[]): void {
  try {
    localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify({
        columns: columns.map(({ id, visible, width }) => ({
          id,
          visible,
          width,
        })),
      }),
    );
  } catch (error) {
    console.warn("保存表格列配置失败:", error);
  }
}

export function useItemTableConfig() {
  const [columns, setColumns] = useState<LibraryColumn[]>(readStoredColumns);

  const updateColumns = useCallback(
    (updater: (previous: LibraryColumn[]) => LibraryColumn[]) => {
      setColumns((previous) => {
        const next = updater(previous);
        writeStoredColumns(next);
        return next;
      });
    },
    [],
  );

  const toggleColumn = useCallback(
    (id: LibraryColumnId) => {
      updateColumns((previous) =>
        previous.map((column) =>
          column.id === id ? { ...column, visible: !column.visible } : column,
        ),
      );
    },
    [updateColumns],
  );

  const resizeColumn = useCallback(
    (id: LibraryColumnId, width: number) => {
      updateColumns((previous) =>
        previous.map((column) =>
          column.id === id
            ? { ...column, width: clampColumnWidth(column, width) }
            : column,
        ),
      );
    },
    [updateColumns],
  );

  const resetColumns = useCallback(() => {
    try {
      localStorage.removeItem(STORAGE_KEY);
    } catch (error) {
      console.warn("重置表格列配置失败:", error);
    }
    setColumns(DEFAULT_LIBRARY_COLUMNS);
  }, []);

  // 声明顺序天然满足「左吸附 → 常规列 → 右吸附」
  const visibleColumns = useMemo(
    () => columns.filter((column) => column.visible),
    [columns],
  );

  return { columns, visibleColumns, toggleColumn, resizeColumn, resetColumns };
}
