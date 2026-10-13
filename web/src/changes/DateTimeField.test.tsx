import { act, useState } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, expect, it, vi } from "vitest";
import { DateTimeField } from "./DateTimeField";
afterEach(() => vi.unstubAllGlobals());
it("requires an explicit autumn instant and preserves UTC when zone input is invalid", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  let change!: (value: string | null) => void, zone!: (value: string) => void;
  function Screen() {
    const [v, set] = useState<string | null>("2027-10-31T00:30:00Z"),
      [z, setZ] = useState("Europe/Warsaw");
    change = set;
    zone = setZ;
    return <DateTimeField label="Termin" value={v} zone={z} onChange={set} />;
  }
  const host = document.createElement("div"),
    root = createRoot(host);
  try {
    await act(async () => root.render(<Screen />));
    expect(host.querySelector("input")!.value).toBe("2027-10-31T02:30");
    expect(host.querySelector("select")!.options.length).toBe(3);
    expect(host.querySelector("select")!.value).toBe(
      "2027-10-31T00:30:00.000Z",
    );
    await act(async () => change(null));
    expect(host.querySelector("select")?.value ?? "").toBe("");
    await act(async () => {
      change("2027-10-31T01:30:00Z");
      zone("Invalid/Zone");
    });
    expect(host.textContent).toContain("UTC 2027-10-31T01:30:00Z");
    await act(async () => zone("Europe/Warsaw"));
    expect(host.querySelector("select")!.value).toBe(
      "2027-10-31T01:30:00.000Z",
    );
  } finally {
    await act(async () => root.unmount());
  }
});
