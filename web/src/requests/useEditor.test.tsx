import { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, expect, it, vi } from "vitest";
import { useEditor } from "./useEditor";
afterEach(() => vi.unstubAllGlobals());
const response = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });

it("reconciles a lost write response with the exact original command and key", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const fetch = vi
    .fn()
    .mockRejectedValueOnce(new TypeError("Network interrupted"))
    .mockResolvedValueOnce(response({ version: "9007199254740995" }));
  vi.stubGlobal("fetch", fetch);
  let ed!: ReturnType<typeof useEditor<{ version: string }>>;
  function Screen() {
    ed = useEditor({ version: "9007199254740993" }, "/problems/1");
    return null;
  }
  const root = createRoot(document.createElement("div"));
  try {
    await act(async () => root.render(<Screen />));
    await act(async () => {
      expect(
        await ed.save(
          "/problems/1/transition",
          { version: "9007199254740993", state: "ASSESS" },
          "POST",
          { "Idempotency-Key": "original-key" },
        ),
      ).toBe(false);
    });
    expect(ed.uncertain).toBe(true);
    await act(async () => {
      expect(
        await ed.save("/problems/1", { title: "different" }, "PUT", {
          "Idempotency-Key": "different-key",
        }),
      ).toBe(true);
    });
    expect(fetch.mock.calls[1]?.[0]).toBe(fetch.mock.calls[0]?.[0]);
    expect(fetch.mock.calls[1]?.[1]?.body).toBe(fetch.mock.calls[0]?.[1]?.body);
    expect(fetch.mock.calls[1]?.[1]?.method).toBe("POST");
    expect(
      new Headers(fetch.mock.calls[1]?.[1]?.headers).get("Idempotency-Key"),
    ).toBe("original-key");
    expect(ed.uncertain).toBe(false);
  } finally {
    await act(async () => root.unmount());
  }
});

it("keeps the original decision version until the current state is explicitly accepted", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_path: string, init?: RequestInit) =>
      init?.method === "POST"
        ? response(
            { error: { code: "WORKFLOW_CONFLICT", message: "Conflict" } },
            409,
          )
        : response({ version: "9007199254740995", state: "WAITING" }),
    ),
  );
  let ed!: ReturnType<typeof useEditor<{ version: string; state: string }>>;
  function Screen() {
    ed = useEditor(
      { version: "9007199254740993", state: "READY" },
      "/workflow/A",
    );
    return null;
  }
  const root = createRoot(document.createElement("div"));
  try {
    await act(async () => root.render(<Screen />));
    await act(async () => {
      expect(
        await ed.save("/workflow/A/decision", {
          reason: "local proposal",
          version: ed.record.version,
        }),
      ).toBe(false);
    });
    expect(ed.record.version).toBe("9007199254740993");
    expect(ed.conflict?.version).toBe("9007199254740995");
    await act(async () => ed.resolve());
    expect(ed.record.version).toBe("9007199254740995");
    expect(ed.conflict).toBeUndefined();
  } finally {
    await act(async () => root.unmount());
  }
});

it("does not report a late decision as successful after changing the execution", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  let finish!: (response: Response) => void;
  vi.stubGlobal(
    "fetch",
    vi.fn(
      () =>
        new Promise<Response>((resolve) => {
          finish = resolve;
        }),
    ),
  );
  let ed!: ReturnType<typeof useEditor<{ version: string }>>;
  function Screen({ id }: { id: string }) {
    ed = useEditor({ version: id }, `/workflow/${id}`);
    return null;
  }
  const root = createRoot(document.createElement("div"));
  try {
    await act(async () => root.render(<Screen key="A" id="A" />));
    let pending!: Promise<boolean>;
    await act(async () => {
      pending = ed.save("/workflow/A/decision", { reason: "A" });
    });
    await act(async () => root.render(<Screen key="B" id="B" />));
    await act(async () => {
      finish(response({ version: "A2" }));
      expect(await pending).toBe(false);
    });
    expect(ed.record.version).toBe("B");
    expect(ed.notice).toBeUndefined();
  } finally {
    await act(async () => root.unmount());
  }
});
