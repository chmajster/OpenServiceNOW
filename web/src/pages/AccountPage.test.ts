import { expect, it } from "vitest";
import { passwordProblem } from "../auth/passwordPolicy";

it("counts Unicode code points and bounds UTF-8 size", () => {
  expect(passwordProblem("😀".repeat(11))).toContain("12");
  expect(passwordProblem("😀".repeat(12))).toBeNull();
  expect(passwordProblem("😀".repeat(256))).toBeNull();
  expect(passwordProblem("😀".repeat(257))).toContain("1024");
});
