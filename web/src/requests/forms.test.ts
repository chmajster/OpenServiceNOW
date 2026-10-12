import { describe, it, expect } from "vitest";
import { cleanValues, type Field } from "./contracts";
describe("catalog visibility", () => {
  it("removes hidden and unknown values including dependent chains", () => {
    const fields: Field[] = [
      { key: "a", label: "A", type: "checkbox", required: false },
      {
        key: "b",
        label: "B",
        type: "text",
        required: false,
        visible_when: { field: "a", equals: true },
      },
      {
        key: "c",
        label: "C",
        type: "text",
        required: false,
        visible_when: { field: "b", equals: "yes" },
      },
    ];
    expect(
      cleanValues(fields, { a: false, b: "yes", c: "secret", unknown: "x" }),
    ).toEqual({ a: false });
    expect(cleanValues(fields, { a: true, b: "yes", c: "visible" })).toEqual({
      a: true,
      b: "yes",
      c: "visible",
    });
  });
});
