export type Window = { start: string; end: string };
export type Calendar = {
  id?: string;
  version?: string;
  name: string;
  timezone: string;
  week: { weekday: number; windows: Window[] }[];
  holidays: string[];
  exceptions: { date: string; windows: Window[] }[];
};
export type Policy = {
  id?: string;
  version?: string;
  name: string;
  enabled: boolean;
  order: number;
  priority: number;
  category: string;
  calendar_id: string;
  calendar_version: string;
  response_seconds: number;
  resolution_seconds: number;
  warning_percent: number;
  pause_reasons: string[];
  effective_at?: string;
};
export type Definition = {
  id: string;
  version: string;
  kind: "policy" | "calendar";
  effective_at: string;
  config: Calendar | Policy;
};
export type DefinitionPage = {
  items: Definition[];
  next_before_id?: string;
  rules: string;
};
export type Instance = {
  id: string;
  incident_id: string;
  policy_id: string;
  policy_version: string;
  policy_name: string;
  calendar_id: string;
  calendar_version: string;
  cycle: string;
  kind: "response" | "resolution";
  status: string;
  started_at: string;
  accrued_seconds: number;
  target_seconds: number;
  remaining_seconds: number;
  due_at: string | null;
  finished_at: string | null;
  paused_at: string | null;
  pause_reason: string;
  warned_at: string | null;
  breached_at: string | null;
  calculated_at: string;
  incident_version: string;
  version: string;
};
export type InstancePage = {
  items: Instance[];
  pending: boolean;
  rules: string;
};
export type HistoryPage = {
  items: {
    kind: "response" | "resolution";
    cycle: string;
    policy_version: string;
    calendar_version: string;
    target_seconds: number;
    id: string;
    instance_id: string;
    incident_version: string;
    source_event_id: string;
    type: string;
    occurred_at: string;
    accrued_seconds: number;
    reason: string;
    due_at: string | null;
  }[];
  next_before_id?: string;
};
export const kindLabel = (kind: string) =>
  kind === "response" ? "Pierwsza odpowiedź" : "Rozwiązanie";
export const statusLabel = (status: string) =>
  ({
    ACTIVE: "Naliczanie",
    PAUSED: "Pauza",
    COMPLETED: "Zakończone",
    CANCELLED: "Anulowane",
    UNANSWERED: "Zakończone bez odpowiedzi",
  })[status] ?? status;
export const workingTime = (seconds: number) =>
  `${Math.floor(seconds / 3600)} godz. ${Math.floor((seconds % 3600) / 60)} min ${seconds % 60} s roboczych`;
export function parseWindows(text: string): Window[] {
  if (!text.trim()) return [];
  let previous = -1;
  return text.split(";").map((value) => {
    const pair = value.trim().split("-");
    const minute = (s: string) =>
      /^([01]\d|2[0-3]):[0-5]\d$/.test(s)
        ? Number(s.slice(0, 2)) * 60 + Number(s.slice(3))
        : s === "24:00"
          ? 1440
          : NaN;
    const [start, end] = pair.map((s) => s.trim());
    const a = minute(start ?? ""),
      b = minute(end ?? "");
    if (
      pair.length !== 2 ||
      !Number.isFinite(a) ||
      !Number.isFinite(b) ||
      a >= b ||
      a < previous
    )
      throw new Error(
        "Godziny wpisz jako 09:00-17:00; przedziały muszą być rosnące i nie mogą się nakładać.",
      );
    previous = b;
    return { start: start!, end: end! };
  });
}
export const windowsText = (windows: Window[]) =>
  windows.map((w) => `${w.start}-${w.end}`).join("; ");
