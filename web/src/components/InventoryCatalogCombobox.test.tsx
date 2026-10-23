import { act, useState } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, expect, it, vi } from "vitest";
import { InventoryCatalogCombobox } from "./InventoryCatalogCombobox";

afterEach(() => vi.unstubAllGlobals());

it("searches saved manufacturers and saves a new value for later reuse", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const requests: { path: string; method: string; body?: string }[] = [];
  vi.stubGlobal("fetch", vi.fn(async (path: string, init: RequestInit = {}) => {
    requests.push({ path, method: init.method ?? "GET", body: init.body?.toString() });
    const url = new URL(path, window.location.origin);
    const payload = init.method === "POST"
      ? { id: "12", kind: "MANUFACTURER", value: "Acer" }
      : url.searchParams.get("q") === "Acer"
        ? { items: [], next_before_id: "" }
        : { items: [{ id: "11", kind: "MANUFACTURER", value: "Dell" }], next_before_id: "" };
    return new Response(JSON.stringify(payload), { headers: { "Content-Type": "application/json" } });
  }));

  function Harness() {
    const [value, setValue] = useState("");
    return <InventoryCatalogCombobox kind="manufacturers" value={value} onChange={setValue} />;
  }

  const container = document.createElement("div");
  document.body.appendChild(container);
  const root = createRoot(container);
  try {
    await act(async () => root.render(<Harness />));
    const input = container.querySelector("input")!;
    await act(async () => {
      input.focus();
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
    expect(container.textContent).toContain("Dell");

    await act(async () => {
      const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
      setter?.call(input, "Acer");
      input.dispatchEvent(new Event("input", { bubbles: true }));
      await new Promise((resolve) => setTimeout(resolve, 240));
    });
    const add = Array.from(container.querySelectorAll("button")).find((button) => button.textContent?.includes("Dodaj"));
    expect(add).toBeDefined();
    await act(async () => {
      add!.click();
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    expect(input.value).toBe("Acer");
    expect(requests.some((request) => request.method === "POST" && request.path === "/api/v1/cmdb/lookups/assets/manufacturers" && request.body === JSON.stringify({ value: "Acer" }))).toBe(true);
  } finally {
    await act(async () => root.unmount());
    container.remove();
  }
});
