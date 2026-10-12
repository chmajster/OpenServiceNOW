import { expect, it } from "vitest";
import { auditChanges, displayValue } from "./auditPresentation";
it("shows only changed fields, including nested and removed fields", () => {
  expect(
    auditChanges(
      { name: "Old", same: 1, nested: { active: true }, removed: "yes" },
      { name: "New", same: 1, nested: { active: false }, roles: ["42"] },
    ),
  ).toEqual([
    { field: "name", before: "Old", after: "New" },
    { field: "nested.active", before: true, after: false },
    { field: "removed", before: "yes", after: undefined },
    { field: "roles", before: undefined, after: ["42"] },
  ]);
  expect(displayValue(false)).toBe("Nie");
  expect(displayValue(["9007199254740993"])).toBe("9007199254740993");
});
