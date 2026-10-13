export type ProblemFields = {
  title: string;
  description: string;
  impact: string;
  symptoms: string;
  analysis: string;
  root_cause: string;
  workaround: string;
  solution_plan: string;
  verification: string;
  known_error: boolean;
  assigned_to: string | null;
  assignment_group_id: string | null;
};
export type Problem = ProblemFields & {
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
  can_known_error: boolean;
  can_tasks: boolean;
  actions: string[];
};
export type SourceIncident = {
  id: string;
  number: string;
  title: string;
  description: string;
  impact: string;
  state: string;
  version: string;
  can_link: boolean;
};
export type AnalyticalTask = {
  id: string;
  problem_id: string;
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
  "ROOT_CAUSE",
  "FIX_IN_PROGRESS",
  "RESOLVED",
  "CLOSED",
  "CANCELLED",
];
export const labels: Record<string, string> = {
  NEW: "Rejestracja",
  ASSESS: "Ocena",
  ROOT_CAUSE: "Analiza przyczyny",
  FIX_IN_PROGRESS: "Przygotowanie rozwiązania",
  RESOLVED: "Rozwiązany",
  CLOSED: "Zamknięty",
  CANCELLED: "Anulowany",
  HIGH: "Wysoki",
  MEDIUM: "Średni",
  LOW: "Niski",
  OPEN: "Otwarte",
  IN_PROGRESS: "W realizacji",
  COMPLETED: "Ukończone",
  newest: "Najnowsze",
  oldest: "Najstarsze",
  title: "Opis skrócony",
  updated: "Ostatnia aktualizacja",
};
export const blank: ProblemFields = {
  title: "",
  description: "",
  impact: "MEDIUM",
  symptoms: "",
  analysis: "",
  root_cause: "",
  workaround: "",
  solution_plan: "",
  verification: "",
  known_error: false,
  assigned_to: null,
  assignment_group_id: null,
};
export function fields(r: ProblemFields): ProblemFields {
  return Object.fromEntries(
    Object.keys(blank).map((k) => [k, r[k as keyof ProblemFields]]),
  ) as ProblemFields;
}
export function errorText(e: unknown) {
  return e instanceof Error ? e.message : "Nie potwierdzono operacji.";
}
