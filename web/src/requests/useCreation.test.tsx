import { act, useState } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, expect, it, vi } from "vitest";
import { useCreation } from "./useCreation";

afterEach(() => vi.unstubAllGlobals());

it("reuses the same idempotency key after an uncertain create response", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const keys: string[] = [];
  const path = "/api/v1/knowledge/spaces/10/pages";
  const input = { title: "Procedura", body: "Treść", status: "DRAFT" };
  let attempts = 0;
  vi.stubGlobal("fetch", vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
    const headers = new Headers(init?.headers);
    keys.push(headers.get("Idempotency-Key") ?? "");
    attempts += 1;
    if (attempts === 1) throw new TypeError("connection lost after request");
    return new Response(JSON.stringify({ id: "123", name: "Operacje" }), {
      headers: { "Content-Type": "application/json" },
    });
  }));
  let retry!: () => void;
  function Screen() {
    const [saved, setSaved] = useState(false);
    const creation = useCreation<{ id: string; name: string }>(path);
    retry = () => { void creation.create(path, input).then((result) => { if (result) setSaved(true); }); };
    return <div><button onClick={retry}>{creation.uncertain ? "Ponów" : "Utwórz"}</button><span>{creation.error ?? (saved ? "Zapisano" : "")}</span></div>;
  }
  const container = document.createElement("div");
  let root = createRoot(container);
  try {
    await act(async () => root.render(<Screen />));
    await act(async () => { container.querySelector("button")?.click(); await new Promise((resolve) => setTimeout(resolve, 0)); });
    expect(container.textContent).toContain("connection lost after request");
    await act(async () => root.unmount());
    root = createRoot(container);
    await act(async () => root.render(<Screen />));
    expect(container.querySelector("button")?.textContent).toBe("Ponów");
    await act(async () => { container.querySelector("button")?.click(); await new Promise((resolve) => setTimeout(resolve, 0)); });
    expect(container.textContent).toContain("Zapisano");
    expect(keys).toHaveLength(2);
    expect(keys[0]).toBeTruthy();
    expect(keys[1]).toBe(keys[0]);
  } finally {
    await act(async () => root.unmount());
  }
});
