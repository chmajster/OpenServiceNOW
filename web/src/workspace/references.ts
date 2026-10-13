import { labels as problemLabels } from "../problems/contracts";
import { labels as changeLabels } from "../changes/contracts";
import { labels as ciLabels } from "../cmdb/contracts";
import { states as requestLabels } from "../requests/contracts";
export type Reference = {
  id: string;
  version: string;
  type: string;
  number: string;
  title: string;
  state: string;
  url: string;
  exact: boolean;
  class?: string;
};
export const referenceTypes: Record<string, string> = {
  incident: "Incydent",
  request: "Wniosek",
  requested_item: "Zamówiona pozycja",
  fulfillment_task: "Zadanie realizacji",
  problem: "Problem",
  problem_task: "Zadanie analityczne",
  change: "Zmiana",
  change_task: "Zadanie wykonawcze",
  ci: "Element konfiguracji",
  catalog_item: "Pozycja katalogu zamówień",
  workflow_execution: "Proces i akceptacje",
};
export function referenceType(r: Reference) {
  return r.type === "ci" && r.class === "SERVICE"
    ? "Usługa biznesowa"
    : (referenceTypes[r.type] ?? r.type);
}
export function referenceState(r: Reference) {
  return referenceStateLabel(r.type, r.state);
}

export function referenceStateLabel(type: string, state: string): string {
  const incident: Record<string, string> = {
    NEW: "Nowy",
    IN_PROGRESS: "W realizacji",
    ON_HOLD: "Wstrzymany",
    RESOLVED: "Rozwiązany",
    CLOSED: "Zamknięty",
    CANCELLED: "Anulowany",
  };
  const workflow: Record<string, string> = {
    READY: "Gotowy",
    WAITING: "Oczekuje",
    ERROR: "Błąd procesu",
    COMPLETED: "Ukończony",
    CANCELLED: "Anulowany",
  };
  const labels = type.startsWith("problem")
    ? problemLabels
    : type.startsWith("change")
      ? changeLabels
      : type === "ci"
        ? ciLabels
        : type === "workflow_execution"
          ? workflow
          : type === "incident"
            ? incident
            : requestLabels;
  return state === "PUBLISHED" ? "Opublikowana" : (labels[state] ?? state);
}
