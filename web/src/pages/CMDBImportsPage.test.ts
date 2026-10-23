import { describe, expect, it } from "vitest";
import { parseCSVHeader } from "./CMDBImportsPage";

describe("parseCSVHeader", () => {
  it("reads a UTF-8 BOM, quoted commas and escaped quotes before CRLF data", () => {
    expect(
      parseCSVHeader('\uFEFF"asset_tag","name","model, ref","vendor"\r\n1,Server,"ACME ""Blue""",North'),
    ).toEqual(["asset_tag", "name", "model, ref", "vendor"]);
  });

  it("reads a single-line header and preserves spaces inside quoted cells", () => {
    expect(parseCSVHeader('"asset tag",name,"serial number"')).toEqual([
      "asset tag",
      "name",
      "serial number",
    ]);
  });

  it("rejects duplicate columns", () => {
    expect(() => parseCSVHeader("name,name")).toThrow();
  });

  it.each(["name,", '"name,asset_tag', '"name"x,asset_tag'])(
    "rejects malformed header %s",
    (header) => {
      expect(() => parseCSVHeader(header)).toThrow();
    },
  );

  it("rejects a quoted empty column", () => {
    expect(() => parseCSVHeader('name,""')).toThrow();
  });

  it("rejects empty headers and names longer than the server limit", () => {
    expect(() => parseCSVHeader("")).toThrow();
    expect(() => parseCSVHeader("x".repeat(161))).toThrow();
  });
});
