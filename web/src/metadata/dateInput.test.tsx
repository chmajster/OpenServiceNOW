import { afterEach, describe, expect, it } from "vitest";
import {
  dateInputCandidates,
  dateInputValue,
  parseDateInput,
} from "./dateInput";
afterEach(() => {
  delete document.documentElement.dataset.timeZone;
});
describe("organization date inputs", () => {
  it("round trips the organization's zone independently of the browser", () => {
    document.documentElement.dataset.timeZone = "Europe/Warsaw";
    expect(dateInputValue("2026-01-12T12:30:00Z")).toBe("2026-01-12T13:30:00");
    expect(parseDateInput("2026-01-12T13:30:00")).toBe(
      "2026-01-12T12:30:00.000Z",
    );
  });
  it("rejects nonexistent time and requires an explicit repeated-hour offset", () => {
    document.documentElement.dataset.timeZone = "Europe/Warsaw";
    expect(() => parseDateInput("2026-03-29T02:30:00")).toThrow();
    expect(dateInputCandidates("2026-10-25T02:30:00")).toHaveLength(2);
    expect(() => parseDateInput("2026-10-25T02:30:00")).toThrow();
    const exact = dateInputValue("2026-10-25T01:30:00Z");
    expect(parseDateInput(exact)).toBe("2026-10-25T01:30:00.000Z");
  });
  it("rejects impossible calendar dates", () => {
    document.documentElement.dataset.timeZone = "UTC";
    expect(() => parseDateInput("2026-02-30T10:00:00")).toThrow();
  });
});
