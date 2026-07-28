import type { Key, ReactNode } from "react";

import { cn } from "@/lib/cn";

export type DataTableColumn<Row> = {
  align?: "left" | "right";
  header: string;
  id: string;
  render: (row: Row) => ReactNode;
};

export function DataTable<Row>({
  caption,
  columns,
  emptyMessage = "暂无数据",
  getRowKey,
  rows,
}: Readonly<{
  caption: string;
  columns: readonly DataTableColumn<Row>[];
  emptyMessage?: string;
  getRowKey: (row: Row) => Key;
  rows: readonly Row[];
}>) {
  return (
    <div className="overflow-hidden rounded-xl border border-border bg-card">
      <div className="overflow-x-auto">
        <table className="w-full border-collapse text-sm">
          <caption className="sr-only">{caption}</caption>
          <thead className="bg-muted/60 text-muted-foreground">
            <tr>
              {columns.map((column) => (
                <th
                  className={cn(
                    "h-10 px-4 text-left text-xs font-medium",
                    column.align === "right" ? "text-right" : null,
                  )}
                  key={column.id}
                  scope="col"
                >
                  {column.header}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.length > 0 ? (
              rows.map((row) => (
                <tr
                  className="border-t border-border transition-colors hover:bg-muted/30"
                  key={getRowKey(row)}
                >
                  {columns.map((column) => (
                    <td
                      className={cn(
                        "px-4 py-3 align-middle",
                        column.align === "right" ? "text-right" : null,
                      )}
                      key={column.id}
                    >
                      {column.render(row)}
                    </td>
                  ))}
                </tr>
              ))
            ) : (
              <tr>
                <td
                  className="h-32 px-4 text-center text-muted-foreground"
                  colSpan={columns.length}
                >
                  {emptyMessage}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
