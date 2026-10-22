import { describe, expect, it } from "vitest";
import {
  buildApprovalWorkflow,
  initialApprovalWizardValues,
  validateApprovalWizardStep,
} from "./wizard";

describe("approval workflow wizard", () => {
  it("requires an executor and realization group before continuing", () => {
    const values = { ...initialApprovalWizardValues, name: "Zakup" };
    expect(validateApprovalWizardStep(0, values)).toMatch(/tożsamość wykonawczą/);
    expect(
      validateApprovalWizardStep(0, {
        ...values,
        executor_id: "12",
      }),
    ).toMatch(/grupę realizacji/);
  });

  it("rejects invalid time limits and overlong task data", () => {
    expect(
      validateApprovalWizardStep(1, {
        ...initialApprovalWizardValues,
        approval_target_id: "17",
        approval_hours: "2161",
      }),
    ).toMatch(/1 do 2160/);
    expect(
      validateApprovalWizardStep(2, {
        ...initialApprovalWizardValues,
        fulfillment_days: "1.5",
      }),
    ).toMatch(/1 do 365/);
  });

  it("builds a complete approval, fulfillment, cancellation and notification graph", () => {
    const config = buildApprovalWorkflow({
      ...initialApprovalWizardValues,
      name: "  Zakup sprzętu  ",
      executor_id: "10",
      group_id: "20",
      approval_target_type: "GROUP",
      approval_target_id: "30",
      approval_policy: "ALL",
      approval_hours: "48",
      fulfillment_title: "  Przygotuj komputer  ",
      fulfillment_instructions: "Skonfiguruj konto.",
      fulfillment_days: "5",
    });
    const byID = Object.fromEntries(config.nodes.map((node) => [node.id, node]));

    expect(config).toMatchObject({
      name: "Zakup sprzętu",
      executor_id: "10",
      group_id: "20",
      self_approval: "DENY",
    });
    expect(byID.approval).toMatchObject({
      target_type: "GROUP",
      target_id: "30",
      policy: "ALL",
      seconds: 172800,
      next: "task",
      rejected: "cancel",
      timeout: "cancel",
    });
    expect(byID.task).toMatchObject({
      type: "CREATE_TASK",
      title: "Przygotuj komputer",
      instructions: "Skonfiguruj konto.",
      next: "wait",
    });
    expect(byID.wait).toMatchObject({
      task_ref: "task",
      seconds: 432000,
      next: "complete",
      timeout: "cancel",
    });
    expect(byID.complete).toMatchObject({ state: "FULFILLED", next: "notify" });
    expect(byID.cancel).toMatchObject({ state: "CANCELLED", next: "notify" });
    expect(byID.notify).toMatchObject({ audience: "REQUESTER", next: "end" });
    expect(byID.end).toMatchObject({ type: "END" });
  });
});
