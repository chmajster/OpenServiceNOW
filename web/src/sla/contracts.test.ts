import { describe, expect, it } from "vitest";
import { parseWindows, workingTime } from "./contracts";

describe("SLA form and business time", () => {
  it("accepts split local windows and explicit day closure", () => {
    expect(parseWindows("09:00-12:00; 13:00-24:00")).toEqual([
      { start: "09:00", end: "12:00" },
      { start: "13:00", end: "24:00" },
    ]);
    expect(parseWindows(" ")).toEqual([]);
  });
  it("rejects overlap, backward windows and ambiguous syntax", () => {
    for (const text of [
      "17:00-09:00",
      "09:00-13:00;12:00-17:00",
      "9:00-17:00",
      "24:00-24:00",
      "08:00",
    ])
      expect(() => parseWindows(text)).toThrow();
  });
  it("reports remaining business duration from the durable snapshot", () =>
    expect(workingTime(3661)).toBe("1 godz. 1 min 1 s roboczych"));
});
