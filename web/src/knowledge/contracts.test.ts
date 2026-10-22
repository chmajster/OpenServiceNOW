import { expect, it } from "vitest";
import {
  articleBodyByteLength,
  knowledgeSearchQueryLength,
  MAX_ARTICLE_BODY_BYTES,
} from "./contracts";

it("counts article size using UTF-8 bytes rather than JavaScript characters", () => {
  expect(articleBodyByteLength("aą🙂")).toBe(7);
  expect(articleBodyByteLength("a".repeat(MAX_ARTICLE_BODY_BYTES + 1))).toBe(MAX_ARTICLE_BODY_BYTES + 1);
});

it("measures trimmed search queries in Unicode code points", () => {
  expect(knowledgeSearchQueryLength("  a🙂b ")).toBe(3);
  expect(knowledgeSearchQueryLength("  ab ")).toBe(2);
});
