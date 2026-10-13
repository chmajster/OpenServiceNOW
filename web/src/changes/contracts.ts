export type ChangeFields = {
  title: string;
  description: string;
  change_type: string;
  impact: string;
  risk: string;
  justification: string;
  scope: string;
  implementation_plan: string;
  test_plan: string;
  rollback_plan: string;
  emergency_reason: string;
  review_result: string;
  review_outcome: string;
  planned_start: string | null;
  planned_end: string | null;
  time_zone: string;
  workflow_id: string | null;
  workflow_version: string | null;
  template_id: string | null;
  template_version: string | null;
  assigned_to: string | null;
  assignment_group_id: string | null;
};
export type Change = ChangeFields & {
  id: string;
  number: string;
  state: string;
  reason: string;
  version: string;
  reporter_id: string;
  reporter_name: string;
  assigned_name: string;
  group_name: string;
  created_at: string;
  updated_at: string;
  can_edit: boolean;
  can_assign: boolean;
  can_tasks: boolean;
  actions: string[];
  assessment_revision: string;
  approved_revision: string | null;
  evaluated_version: string | null;
  execution_id: string | null;
  approval_state: string;
  actual_start: string | null;
  actual_end: string | null;
};
export type SourceProblem = {
  id: string;
  number: string;
  title: string;
  description: string;
  impact: string;
  solution_plan: string;
  state: string;
  version: string;
  can_link: boolean;
};
export type ExecutionTask = {
  id: string;
  change_id: string;
  number: string;
  title: string;
  instructions: string;
  required: boolean;
  state: string;
  result: string;
  version: string;
  assigned_to: string | null;
  assignment_group_id: string | null;
  assigned_name: string;
  group_name: string;
  can_edit: boolean;
  updated_at: string;
};

export const states = [
  "NEW",
  "ASSESS",
  "AUTHORIZE",
  "SCHEDULED",
  "IMPLEMENT",
  "REVIEW",
  "CLOSED",
  "CANCELLED",
];
export const labels: Record<string, string> = {
  NEW: "Rejestracja",
  ASSESS: "Ocena",
  AUTHORIZE: "Autoryzacja",
  SCHEDULED: "Zaplanowana",
  IMPLEMENT: "Wykonanie",
  REVIEW: "Przegląd",
  CLOSED: "Zamknięta",
  CANCELLED: "Anulowana",
  NORMAL: "Normal",
  STANDARD: "Standard",
  EMERGENCY: "Emergency",
  HIGH: "Wysoki",
  MEDIUM: "Średni",
  LOW: "Niski",
  OPEN: "Otwarte",
  IN_PROGRESS: "W realizacji",
  COMPLETED: "Ukończone",
  NONE: "Bez oceny",
  PENDING: "Oczekuje na decyzję",
  APPROVED: "Zaakceptowana",
  REJECTED: "Odrzucona",
  INVALIDATED: "Wymaga ponownej oceny",
  SUCCESS: "Powodzenie",
  PARTIAL: "Częściowe powodzenie",
  FAILED: "Niepowodzenie",
  newest: "Najnowsze",
  oldest: "Najstarsze",
  title: "Opis skrócony",
  updated: "Ostatnia aktualizacja",
};
export const blank: ChangeFields = {
  title: "",
  description: "",
  change_type: "NORMAL",
  impact: "MEDIUM",
  risk: "MEDIUM",
  justification: "",
  scope: "",
  implementation_plan: "",
  test_plan: "",
  rollback_plan: "",
  emergency_reason: "",
  review_result: "",
  review_outcome: "",
  planned_start: null,
  planned_end: null,
  time_zone: "Europe/Warsaw",
  workflow_id: null,
  workflow_version: null,
  template_id: null,
  template_version: null,
  assigned_to: null,
  assignment_group_id: null,
};
export function fields(r: ChangeFields): ChangeFields {
  return Object.fromEntries(
    Object.keys(blank).map((k) => [k, r[k as keyof ChangeFields]]),
  ) as ChangeFields;
}
export function errorText(e: unknown) {
  return e instanceof Error ? e.message : "Nie potwierdzono operacji.";
}
export type TemplateConfig = {
  fields: ChangeFields;
  ci_ids: string[];
  max_risk: string;
  max_duration_seconds: number;
};
export type TemplateSummary = {
  id: string;
  name: string;
  version: string;
  published_version: string | null;
  withdrawn: boolean;
};
export type Template = TemplateSummary & {
  draft: TemplateConfig;
  author_id: string;
};
export type CalendarRule = {
  id: string;
  version: string;
  ci_id: string | null;
  kind: string;
  title: string;
  starts_at: string;
  ends_at: string;
  time_zone: string;
  active: boolean;
};
export type Collision = {
  items: {
    id: string;
    number: string;
    title: string;
    state: string;
    version: string;
  }[];
  restricted_conflict: boolean;
  blackout: boolean;
  outside_window: boolean;
};
