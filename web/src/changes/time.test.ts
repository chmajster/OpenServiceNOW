import { describe, expect, it } from "vitest";
import { localCandidates, localTime } from "./time";

describe("Change dates in IANA zones", () => {
  it("rejects nonexistent spring time and preserves both autumn instants", () => {
    expect(localCandidates("2027-03-28T02:30", "Europe/Warsaw")).toEqual([]);
    const twice = localCandidates("2027-10-31T02:30", "Europe/Warsaw");
    expect(twice).toEqual([
      "2027-10-31T00:30:00.000Z",
      "2027-10-31T01:30:00.000Z",
    ]);
    for (const instant of twice)
      expect(localTime(instant, "Europe/Warsaw")).toBe("2027-10-31T02:30");
  });
  it("supports half-hour DST and quarter-hour zones without rounding", () => {
    expect(
      localCandidates("2027-04-04T01:45", "Australia/Lord_Howe"),
    ).toHaveLength(2);
    expect(localCandidates("2027-01-01T10:00", "Asia/Kathmandu")).toEqual([
      "2027-01-01T04:15:00.000Z",
    ]);
  });
  it("rejects impossible calendar dates and malformed wall-clock input", () => {
    expect(localCandidates("2027-02-31T10:00", "UTC")).toEqual([]);
    expect(localCandidates("2027-01-01T25:00", "UTC")).toEqual([]);
    expect(localCandidates("", "UTC")).toEqual([]);
  });
});
