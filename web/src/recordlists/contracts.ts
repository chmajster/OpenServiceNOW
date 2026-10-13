export type FilterNode = {
  logic?: "AND" | "OR";
  children?: FilterNode[];
  field?: string;
  operator?: string;
  value?: string;
};
export type FilterField = {
  key: string;
  label: string;
  kind: "text" | "enum" | "id" | "number" | "time" | "boolean";
  options?: { value: string; label: string }[];
  endpoint?: string;
  collection?: string;
  nullable?: boolean;
};
export type ColumnConfig = { key: string; width: number };
export type ListConfig = {
  filter?: FilterNode;
  sort: string;
  group_by?: string;
  columns: ColumnConfig[];
  context?: Record<string, string>;
};
export type SavedView = {
  id: string;
  owner_user_id: string;
  group_id?: string;
  domain: string;
  name: string;
  configuration: ListConfig;
  version: string;
  can_edit: boolean;
};
export function decodeFilter(raw: string | null): FilterNode | undefined {
  if (!raw) return;
  try {
    return JSON.parse(raw) as FilterNode;
  } catch {
    return;
  }
}
export function columnsFrom(
  raw: string | null,
  defaults: ColumnConfig[],
): ColumnConfig[] {
  if (!raw) return defaults;
  try {
    const parsed: unknown = JSON.parse(raw);
    if (
      Array.isArray(parsed) &&
      parsed.length &&
      parsed.length <= 20 &&
      parsed.every(
        (c) =>
          c &&
          typeof c.key === "string" &&
          defaults.some((d) => d.key === c.key) &&
          Number.isInteger(c.width) &&
          c.width >= 80 &&
          c.width <= 600,
      ) &&
      new Set(parsed.map((c) => c.key)).size === parsed.length
    )
      return parsed as ColumnConfig[];
  } catch {
    /* Malformed presentation configuration uses domain defaults. */
  }
  return defaults;
}
