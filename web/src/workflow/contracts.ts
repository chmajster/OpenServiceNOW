import type { Publication, Values } from "../requests/contracts";
export type ProcessNode = {
  id: string;
  label: string;
  type: string;
  next?: string;
  otherwise?: string;
  rejected?: string;
  timeout?: string;
  field?: string;
  equals?: string | number | boolean;
  target_type?: string;
  target_id?: string;
  policy?: string;
  seconds?: number;
  task_ref?: string;
  title?: string;
  instructions?: string;
  state?: string;
  reason?: string;
  audience?: string;
};
export type Config = {
  name: string;
  executor_id: string;
  group_id: string;
  self_approval: string;
  nodes: ProcessNode[];
};
export type Definition = {
  id: string;
  version: string;
  published_version?: string;
  draft: Config;
};
export type Step = {
  node_id: string;
  label: string;
  type: string;
  state: string;
  version: string;
  result?: { outcome?: string; task_id?: string };
  started_at?: string;
  deadline?: string;
  finished_at?: string;
  can_decide: boolean;
  decisions?: {
    user_id: string;
    name: string;
    decision: string;
    reason: string;
    decided_at?: string;
  }[];
};
export type Execution = {
  id: string;
  definition_id: string;
  definition_version: string;
  request_id: string;
  requested_item_id: string;
  executor_id: string;
  group_id: string;
  current_node: string;
  state: string;
  version: string;
  approved: boolean;
  error_code?: string;
  name: string;
  request_number: string;
  created_at: string;
  updated_at: string;
  steps: Step[];
  actions: string[];
  snapshot?: Publication;
  values?: Values;
  attempts?: {
    id: string;
    node_id: string;
    execution_version: string;
    outcome: string;
    error_code?: string;
    occurred_at: string;
  }[];
};
export const labels: Record<string, string> = {
  START: "Start",
  CONDITION: "Warunek",
  APPROVAL: "Akceptacja",
  CREATE_TASK: "Utwórz zadanie",
  WAIT_TASK: "Oczekuj na zadanie",
  WAIT_TIMER: "Oczekuj na termin",
  UPDATE_RECORD: "Zmień stan wniosku",
  NOTIFY: "Powiadom zgłaszającego",
  END: "Koniec",
  READY: "Gotowy do wykonania",
  WAITING: "Oczekiwanie",
  ERROR: "Zatrzymany przez błąd",
  COMPLETED: "Ukończony",
  CANCELLED: "Anulowany",
  NOT_STARTED: "Niewykonany",
  PENDING: "Oczekuje na decyzję",
  APPROVED: "Zaakceptowano",
  REJECTED: "Odrzucono",
  TIMED_OUT: "Upłynął termin",
  TRUE: "Warunek spełniony",
  FALSE: "Warunek niespełniony",
  TASK_COMPLETED: "Zadanie ukończone",
  TIMER_ELAPSED: "Termin osiągnięty",
};
export const errors: Record<string, string> = {
  EXECUTOR_ACCESS_REVOKED:
    "Wykonawca utracił aktywność, członkostwo lub wymagane uprawnienia. Przywróć je przed wznowieniem.",
  INVALID_STEP_CONTEXT:
    "Konfiguracja kroku lub lista uprawnionych akceptujących jest niedostępna. Sprawdź grupę i politykę własnego wniosku.",
  DOMAIN_COMMAND_REJECTED:
    "Komenda domeny została odrzucona lub zapis się nie powiódł. Sprawdź stan wniosku, wymagane zadania i pozostałe procesy.",
};
export const blank: Config = {
  name: "",
  executor_id: "0",
  group_id: "0",
  self_approval: "DENY",
  nodes: [
    { id: "start", label: "Złożenie", type: "START", next: "approval" },
    {
      id: "approval",
      label: "Akceptacja wniosku",
      type: "APPROVAL",
      target_type: "GROUP",
      target_id: "0",
      policy: "ANY",
      seconds: 86400,
      next: "task",
      rejected: "cancel",
      timeout: "cancel",
    },
    {
      id: "task",
      label: "Zadanie realizacji",
      type: "CREATE_TASK",
      title: "Realizacja usługi",
      instructions: "",
      next: "wait",
    },
    {
      id: "wait",
      label: "Oczekiwanie na realizację",
      type: "WAIT_TASK",
      task_ref: "task",
      seconds: 604800,
      next: "complete",
      timeout: "cancel",
    },
    {
      id: "complete",
      label: "Zrealizowanie wniosku",
      type: "UPDATE_RECORD",
      state: "FULFILLED",
      reason: "Ukończono wymagane zadania po akceptacji",
      next: "notify",
    },
    {
      id: "notify",
      label: "Powiadomienie",
      type: "NOTIFY",
      audience: "REQUESTER",
      next: "end",
    },
    {
      id: "cancel",
      label: "Anulowanie wniosku",
      type: "UPDATE_RECORD",
      state: "CANCELLED",
      reason: "Odrzucenie akceptacji lub przekroczenie terminu",
      next: "notify",
    },
    { id: "end", label: "Koniec", type: "END" },
  ],
};
