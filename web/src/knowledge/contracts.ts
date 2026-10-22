export type Space = {
  id: string;
  key: string;
  name: string;
  description: string;
  status: "ACTIVE" | "ARCHIVED";
  review_interval_days: number;
  version: string;
  page_count: string;
  watched: boolean;
  created_at: string;
  updated_at: string;
};

export type PageSummary = {
  id: string;
  space_id: string;
  parent_id?: string;
  title: string;
  status: "DRAFT" | "PUBLISHED";
  review_due_at?: string;
  version: string;
  updated_by: string;
  updated_by_name: string;
  updated_at: string;
  excerpt?: string;
  attention_reason?: "DRAFT" | "REVIEW_DUE";
};

export type KnowledgePageRecord = PageSummary & {
  body: string;
  created_by: string;
  created_at: string;
};

export type PageVersion = {
  page_id: string;
  version: string;
  parent_id?: string;
  title: string;
  status: "DRAFT" | "PUBLISHED";
  review_due_at?: string;
  changed_by: string;
  changed_by_name: string;
  changed_at: string;
};

export type ImportResult = {
  space_id: string;
  created: number;
  replayed: boolean;
};

export const pageStatus: Record<PageSummary["status"], string> = {
  DRAFT: "Szkic",
  PUBLISHED: "Opublikowana",
};

export const attentionLabel: Record<string, string> = {
  DRAFT: "Szkic wymaga publikacji",
  REVIEW_DUE: "Minął termin przeglądu",
};
