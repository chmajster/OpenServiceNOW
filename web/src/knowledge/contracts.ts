export type Space = {
  id: string;
  key: string;
  name: string;
  description: string;
  status: "ACTIVE" | "ARCHIVED";
  review_interval_days: number;
  version: string;
  page_count: string;
  subscribed: boolean;
  created_at: string;
  updated_at: string;
};

export type ArticleStatus = "DRAFT" | "PUBLISHED" | "RETIRED";

export type PageSummary = {
  id: string;
  space_id: string;
  title: string;
  status: ArticleStatus;
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
  title: string;
  status: ArticleStatus;
  review_due_at?: string;
  changed_by: string;
  changed_by_name: string;
  changed_at: string;
};

export const pageStatus: Record<ArticleStatus, string> = {
  DRAFT: "Nieopublikowany",
  PUBLISHED: "Opublikowany",
  RETIRED: "Wycofany",
};

export function articleNumber(id: string) {
  return `KB${id.padStart(7, "0")}`;
}
