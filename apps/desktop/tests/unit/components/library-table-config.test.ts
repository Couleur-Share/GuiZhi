import { describe, expect, it } from "vitest";
import {
  DEFAULT_LIBRARY_COLUMNS,
  clampColumnWidth,
  mergeStoredColumns,
} from "../../../src/renderer/components/library/item-table-config";

function columnById(id: string) {
  const column = DEFAULT_LIBRARY_COLUMNS.find(
    (candidate) => candidate.id === id,
  );
  if (!column) {
    throw new Error(`默认列定义里没有 ${id}`);
  }
  return column;
}

describe("列表视图列配置", () => {
  it("没有持久化时用默认列", () => {
    expect(mergeStoredColumns(null)).toBe(DEFAULT_LIBRARY_COLUMNS);
    expect(mergeStoredColumns({ columns: "bad" })).toBe(
      DEFAULT_LIBRARY_COLUMNS,
    );
  });

  it("只恢复显示状态与列宽，其余属性以默认定义为准", () => {
    const merged = mergeStoredColumns({
      columns: [
        {
          id: "snippet",
          visible: false,
          width: 200,
          minWidth: 1,
          resizable: false,
          sticky: "right",
        },
      ],
    });
    const snippet = merged.find((column) => column.id === "snippet");
    expect(snippet).toMatchObject({
      visible: false,
      width: 200,
      minWidth: columnById("snippet").minWidth,
      resizable: true,
    });
    expect(snippet?.sticky).toBeUndefined();
  });

  it("列宽越界时夹回默认定义的区间", () => {
    const title = columnById("title");
    const merged = mergeStoredColumns({
      columns: [
        { id: "title", width: 10 },
        { id: "tags", width: 9999 },
      ],
    });
    expect(merged.find((column) => column.id === "title")?.width).toBe(
      title.minWidth,
    );
    expect(merged.find((column) => column.id === "tags")?.width).toBe(
      columnById("tags").maxWidth,
    );
  });

  it("未知列 id 与坏数据不影响默认列", () => {
    const merged = mergeStoredColumns({
      columns: [{ id: "gallery", visible: true }, null, "oops"],
    });
    expect(merged).toHaveLength(DEFAULT_LIBRARY_COLUMNS.length);
    expect(merged).toEqual(DEFAULT_LIBRARY_COLUMNS);
  });

  it("clampColumnWidth 对非数字回落到默认宽度", () => {
    const title = columnById("title");
    expect(clampColumnWidth(title, Number.NaN)).toBe(title.width);
    expect(clampColumnWidth(title, "260" as never)).toBe(title.width);
  });
});
