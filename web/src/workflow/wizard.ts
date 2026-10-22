import type { Config } from "./contracts";

export type ApprovalWizardValues = {
  name: string;
  executor_id: string;
  group_id: string;
  self_approval: "DENY" | "ALLOW";
  approval_target_type: "USER" | "GROUP";
  approval_target_id: string;
  approval_policy: "ANY" | "ALL";
  approval_hours: string;
  fulfillment_title: string;
  fulfillment_instructions: string;
  fulfillment_days: string;
};

export const initialApprovalWizardValues: ApprovalWizardValues = {
  name: "",
  executor_id: "0",
  group_id: "0",
  self_approval: "DENY",
  approval_target_type: "GROUP",
  approval_target_id: "0",
  approval_policy: "ANY",
  approval_hours: "24",
  fulfillment_title: "Realizacja usługi",
  fulfillment_instructions: "",
  fulfillment_days: "7",
};

function integerInRange(value: string, min: number, max: number) {
  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed >= min && parsed <= max;
}

function runeLength(value: string) {
  return Array.from(value).length;
}

export function validateApprovalWizardStep(
  step: number,
  values: ApprovalWizardValues,
): string | undefined {
  if (step === 0) {
    if (!values.name.trim() || runeLength(values.name.trim()) > 200)
      return "Podaj nazwę procesu (maksymalnie 200 znaków).";
    if (values.executor_id === "0")
      return "Wybierz aktywną tożsamość wykonawczą.";
    if (values.group_id === "0")
      return "Wybierz grupę realizacji procesu.";
  }
  if (step === 1) {
    if (values.approval_target_id === "0")
      return values.approval_target_type === "GROUP"
        ? "Wybierz grupę akceptującą."
        : "Wybierz osobę akceptującą.";
    if (!integerInRange(values.approval_hours, 1, 2160))
      return "Termin akceptacji musi wynosić od 1 do 2160 godzin.";
  }
  if (step === 2) {
    if (
      !values.fulfillment_title.trim() ||
      runeLength(values.fulfillment_title.trim()) > 200
    )
      return "Podaj tytuł zadania (maksymalnie 200 znaków).";
    if (runeLength(values.fulfillment_instructions) > 10000)
      return "Instrukcje zadania mogą mieć maksymalnie 10 000 znaków.";
    if (!integerInRange(values.fulfillment_days, 1, 365))
      return "Termin realizacji musi wynosić od 1 do 365 dni.";
  }
  return undefined;
}

export function buildApprovalWorkflow(
  values: ApprovalWizardValues,
): Config {
  return {
    name: values.name.trim(),
    executor_id: values.executor_id,
    group_id: values.group_id,
    self_approval: values.self_approval,
    nodes: [
      { id: "start", label: "Złożenie wniosku", type: "START", next: "approval" },
      {
        id: "approval",
        label: "Akceptacja wniosku",
        type: "APPROVAL",
        target_type: values.approval_target_type,
        target_id: values.approval_target_id,
        policy: values.approval_policy,
        seconds: Number(values.approval_hours) * 3600,
        next: "task",
        rejected: "cancel",
        timeout: "cancel",
      },
      {
        id: "task",
        label: "Zadanie realizacji",
        type: "CREATE_TASK",
        title: values.fulfillment_title.trim(),
        instructions: values.fulfillment_instructions.trim(),
        next: "wait",
      },
      {
        id: "wait",
        label: "Oczekiwanie na realizację",
        type: "WAIT_TASK",
        task_ref: "task",
        seconds: Number(values.fulfillment_days) * 86400,
        next: "complete",
        timeout: "cancel",
      },
      {
        id: "complete",
        label: "Zrealizowanie wniosku",
        type: "UPDATE_RECORD",
        state: "FULFILLED",
        reason: "Ukończono wymagane zadanie po akceptacji.",
        next: "notify",
      },
      {
        id: "cancel",
        label: "Anulowanie wniosku",
        type: "UPDATE_RECORD",
        state: "CANCELLED",
        reason: "Wniosek odrzucono albo upłynął termin akceptacji lub realizacji.",
        next: "notify",
      },
      {
        id: "notify",
        label: "Powiadomienie zgłaszającego",
        type: "NOTIFY",
        audience: "REQUESTER",
        next: "end",
      },
      { id: "end", label: "Koniec procesu", type: "END" },
    ],
  };
}
