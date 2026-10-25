export type Locale = "pl" | "en";
export type Option = { value: string; labels: Record<Locale, string> };
export type Expression = {
  op: string;
  field?: string;
  value?: unknown;
  children?: Expression[];
};
export type MetadataField = {
  key: string;
  labels: Record<Locale, string>;
  description?: Partial<Record<Locale, string>>;
  type: "TEXT" | "LONG_TEXT" | "BOOLEAN" | "INTEGER" | "DECIMAL" | "DATE" | "DATETIME" | "CHOICE" | "MULTI_CHOICE" | "REFERENCE";
  max_length?: number;
  precision?: number;
  scale?: number;
  min?: unknown;
  max?: unknown;
  default?: unknown;
  required: boolean;
  unique?: boolean;
  options?: Option[];
  reference_type?: "user" | "group" | "incident" | "request" | "ci" | "asset";
  parent_key?: string;
  active: boolean;
  visible_when?: Expression;
  required_when?: Expression;
  system?: boolean;
};
export type FormSection = { key: string; label: string; labels?: Partial<Record<Locale, string>>; fields: string[]; columns: number };
export type MetadataSchema = {
  type_key: "incident" | "ci";
  version: string;
  fields: MetadataField[];
  form: { sections: FormSection[]; tabs: string[]; role_views?: Record<string, string[]> };
  list: { columns: string[]; default_sort: string; search_fields: string[]; filters: string[]; role_views?: Record<string, string[]> };
  configuration?: Configuration;
};
export type RecordType = {
  key: string;
  labels: Record<Locale, string>;
  parent?: string;
  fields: MetadataField[];
  form: MetadataSchema["form"];
  list: MetadataSchema["list"];
};
export type Organization = {
  name: string;
  default_locale: Locale;
  time_zone: string;
  date_format: "YYYY-MM-DD" | "DD.MM.YYYY" | "MM/DD/YYYY";
  number_format: "1,234.56" | "1.234,56";
  currency: string;
  brand_color: string;
  logo_url?: string;
};
export type Configuration = {
  organization: Organization;
  record_types: Record<string, RecordType>;
  dictionaries: Record<string, Option[]>;
  priority_matrix: number[][];
};
export type MetadataHead = {
  version: string;
  draft_version: string;
  published_version: string;
  draft: Configuration;
  published: Configuration;
};
export type MetadataImpact = {
  can_publish: boolean;
  changes: { key: string; kind: string; count: string; examples?: string[]; message: string }[];
};
export type ReferenceItem = { id: string; label: string };
export type ReferencePage = { items: ReferenceItem[]; next_before_id?: string };
