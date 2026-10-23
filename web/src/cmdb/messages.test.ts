import { afterEach, describe, expect, it, vi } from "vitest";
import { cmdbLocale, setCMDBLocale } from "./messages";

describe("CMDB locale storage", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    window.localStorage.clear();
    document.documentElement.lang = "pl";
  });

  it("falls back to the document language when storage reads are blocked", () => {
    document.documentElement.lang = "en";
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new DOMException("Storage is blocked", "SecurityError");
    });

    expect(cmdbLocale()).toBe("en");
  });

  it("still changes the active language when storage writes are blocked", () => {
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new DOMException("Storage is blocked", "SecurityError");
    });
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new DOMException("Storage is blocked", "SecurityError");
    });
    const onLocaleChange = vi.fn();
    window.addEventListener("opensnow:locale-change", onLocaleChange, { once: true });

    expect(() => setCMDBLocale("en")).not.toThrow();
    expect(document.documentElement.lang).toBe("en");
    expect(cmdbLocale()).toBe("en");
    expect(onLocaleChange).toHaveBeenCalledOnce();
  });
});
