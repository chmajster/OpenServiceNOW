import { act, useState } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, expect, it, vi } from "vitest";
import { useIncidentForm, useIncidentQuery } from "./hooks";
import type { Incident } from "./contracts";

vi.mock("../auth/AuthContext", () => ({
  useAuth: () => ({ session: { tenant: { id: "1" }, user: { id: "2" } } }),
}));
afterEach(() => vi.unstubAllGlobals());
const response = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });

it("never displays a delayed incident A on the current incident B", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const requests = new Map<string, (value: Response) => void>();
  vi.stubGlobal(
    "fetch",
    vi.fn(
      (path: string) =>
        new Promise<Response>((resolve) => requests.set(path, resolve)),
    ),
  );
  let select!: (id: string) => void;
  function Screen() {
    const [id, setID] = useState("A");
    select = setID;
    const result = useIncidentQuery<{ title: string }>(`/incidents/${id}`);
    return (
      <div>
        <button disabled={result.loading}>Edytuj</button>
        <span>{result.data?.title}</span>
      </div>
    );
  }
  const container = document.createElement("div"),
    root = createRoot(container);
  try {
    await act(async () => root.render(<Screen />));
    await act(async () => select("B"));
    expect(container.querySelector("button")?.disabled).toBe(true);
    await act(async () =>
      requests.get("/incidents/B")!(response({ title: "B" })),
    );
    await act(async () =>
      requests.get("/incidents/A")!(response({ title: "A" })),
    );
    expect(container.querySelector("span")?.textContent).toBe("B");
  } finally {
    await act(async () => root.unmount());
  }
});

it("keeps a draft's original version until an explicit conflict decision", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const submitted: { title: string; version: string }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_path: string, init?: RequestInit) => {
      if (init?.method === "PUT") {
        submitted.push(JSON.parse(init.body as string));
        if (submitted.length === 1)
          return response(
            { error: { code: "INCIDENT_CONFLICT", message: "Konflikt" } },
            409,
          );
        return response({ id: "1", title: submitted[1]!.title, version: "5" });
      }
      return response({ id: "1", title: "Current server title", version: "4" });
    }),
  );
  let form!: ReturnType<typeof useIncidentForm<string>>;
  let refresh!: (record: Incident) => void;
  const confirmations: (boolean | undefined)[] = [];
  function Screen() {
    const [record, setRecord] = useState({
      id: "1",
      title: "Original",
      version: "1",
    } as Incident);
    refresh = setRecord;
    form = useIncidentForm(
      record,
      (value) => value.title,
      (value, confirmed) => {
        confirmations.push(confirmed);
        setRecord(value);
      },
      () => {},
    );
    return <span>{form.draft}</span>;
  }
  const container = document.createElement("div"),
    root = createRoot(container);
  const event = { preventDefault() {} } as Parameters<typeof form.submit>[0];
  const save = () =>
    form.submit(event, "/incidents/1", "PUT", () => ({ title: form.draft }));
  try {
    await act(async () => root.render(<Screen />));
    await act(async () => form.setDraft("My local proposal"));
    await act(async () =>
      refresh({
        id: "1",
        title: "Read after another edit",
        version: "3",
      } as Incident),
    );
    expect(form.version).toBe("1");
    expect(form.draft).toBe("My local proposal");
    await act(save);
    expect(submitted).toEqual([{ title: "My local proposal", version: "1" }]);
    expect(form.conflict?.version).toBe("4");
    expect(form.draft).toBe("My local proposal");
    expect(confirmations).toEqual([false]);
    await act(save);
    expect(submitted).toHaveLength(1);
    await act(async () => form.keepProposal());
    expect(form.version).toBe("4");
    await act(save);
    expect(submitted[1]).toEqual({ title: "My local proposal", version: "4" });
    expect(form.version).toBe("5");
    expect(form.dirty).toBe(false);
    expect(form.conflict).toBeUndefined();
  } finally {
    await act(async () => root.unmount());
  }
});
