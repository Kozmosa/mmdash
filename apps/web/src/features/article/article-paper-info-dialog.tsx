import { useState } from "react";
import { useMutation } from "@tanstack/react-query";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";

import { articleApi } from "./api";
import type { ArticlePaperInfo, ArticlePaperInfoField } from "./types";

export const GENERIC_PAPER_FIELDS: ReadonlyArray<{
  key: string;
  label: string;
}> = [
  { key: "title", label: "标题" },
  { key: "author", label: "作者" },
  { key: "date", label: "日期" },
  { key: "abstract", label: "摘要输出" },
  { key: "keywords", label: "关键词" },
];

export const CUMCM_PAPER_FIELDS: ReadonlyArray<{
  key: string;
  label: string;
}> = [
  { key: "problem_number", label: "题号" },
  { key: "team_number", label: "队伍编号" },
  { key: "school", label: "学校" },
  { key: "captain", label: "队长" },
  { key: "member2", label: "队员二" },
  { key: "member3", label: "队员三" },
  { key: "supervisor", label: "指导教师" },
  { key: "submit_date", label: "提交日期" },
];

const FIELD_KEYS = new Set(
  [...GENERIC_PAPER_FIELDS, ...CUMCM_PAPER_FIELDS].map((field) => field.key),
);

export function emptyPaperInfo(): ArticlePaperInfo {
  return { schema_version: "1.0", fields: {} };
}

export function paperInfoFieldValue(
  info: ArticlePaperInfo | undefined,
  key: string,
): ArticlePaperInfoField {
  const field = info?.fields[key];
  return { enabled: field?.enabled ?? false, value: field?.value ?? "" };
}

export function abstractEnabled(info: ArticlePaperInfo | undefined): boolean {
  const field = info?.fields.abstract;
  // Absent choice means enabled: the Worker renders the abstract whenever it
  // has content and the field was not explicitly turned off.
  return field ? field.enabled : true;
}

// The 论文信息 dialog selects which fields this article renders into the
// template. Unselected fields generate no TeX; the template field profile
// decides compatibility at build time.
export function PaperInfoDialog({
  canEdit,
  info,
  onClose,
  projectId,
  showCumcm,
}: Readonly<{
  canEdit: boolean;
  info: ArticlePaperInfo | undefined;
  onClose: () => void;
  projectId: string;
  showCumcm: boolean;
}>) {
  const [fields, setFields] = useState<Record<string, ArticlePaperInfoField>>(
    () => {
      const seed: Record<string, ArticlePaperInfoField> = {};
      for (const key of FIELD_KEYS) seed[key] = paperInfoFieldValue(info, key);
      return seed;
    },
  );
  const update = (key: string, patch: Partial<ArticlePaperInfoField>) =>
    setFields((current) => ({
      ...current,
      [key]: { ...current[key], ...patch },
    }));
  const save = useMutation({
    mutationFn: () =>
      articleApi.updatePaperInfo(projectId, {
        fields: Object.fromEntries(
          Object.entries(fields).filter(([key]) => FIELD_KEYS.has(key)),
        ),
        schema_version: "1.0",
      }),
    onSuccess: onClose,
  });
  const error = save.error;
  const renderField = (field: { key: string; label: string }) => (
    <div className="space-y-1" key={field.key}>
      <label className="flex items-center gap-2 text-sm">
        <input
          checked={fields[field.key]?.enabled ?? false}
          disabled={!canEdit || save.isPending}
          onChange={(event) =>
            update(field.key, { enabled: event.target.checked })
          }
          type="checkbox"
        />
        {field.label}
      </label>
      {fields[field.key]?.enabled && field.key !== "abstract" ? (
        <Input
          aria-label={field.label}
          disabled={!canEdit || save.isPending}
          onChange={(event) => update(field.key, { value: event.target.value })}
          value={fields[field.key]?.value ?? ""}
        />
      ) : null}
    </div>
  );
  return (
    <div
      aria-modal="true"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/45 p-4"
      onMouseDown={(event) => {
        if (event.currentTarget === event.target) onClose();
      }}
      role="dialog"
    >
      <Card className="max-h-[85vh] w-full max-w-xl overflow-y-auto">
        <CardHeader>
          <CardTitle>论文信息</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <p className="text-xs text-muted-foreground">
            勾选本文需要的字段并填写内容；未勾选的字段不会写入
            PDF。不兼容当前模板的字段在构建时会被跳过。
          </p>
          <div className="grid gap-3 sm:grid-cols-2">
            {GENERIC_PAPER_FIELDS.map(renderField)}
          </div>
          {showCumcm ? (
            <div className="border-t pt-3">
              <p className="mb-2 text-sm font-medium">国赛字段</p>
              <div className="grid gap-3 sm:grid-cols-2">
                {CUMCM_PAPER_FIELDS.map(renderField)}
              </div>
            </div>
          ) : null}
          {error ? (
            <p className="text-sm text-destructive">{error.message}</p>
          ) : null}
          <div className="flex justify-end gap-2">
            <Button onClick={onClose} size="sm" variant="outline">
              取消
            </Button>
            <Button
              disabled={!canEdit || save.isPending}
              onClick={() => save.mutate()}
              size="sm"
            >
              保存
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
