import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { MemoryRouter } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { WorkRecordEditor } from "./WorkRecordEditor";

const fixtures = vi.hoisted(() => ({
  incident: { id: "31", number: "INC0031" },
  metadata: { states: [], categories: [], resolution_codes: [], priority_matrix: [], can_create: false, custom_fields_schema: { version: "1", fields: [], list: { columns: [], filters: [], default_sort: "updated_desc" } } },
}));

vi.mock("../auth/AuthContext", () => ({
  useAuth: () => ({ hasPermission: () => true }),
}));

vi.mock("../incidents/hooks", () => ({
  useIncidentQuery: (path: string) => ({
    loading: false,
    data: path.endsWith("/metadata") ? fixtures.metadata : fixtures.incident,
    reload: vi.fn(),
  }),
}));

vi.mock("../incidents/IncidentWorkspace", () => ({
  IncidentWorkspace: (props: { initial: { number: string }; embedded: boolean; onDirtyChange?: (dirty: boolean) => void }) => (
    <button type="button" onClick={() => props.onDirtyChange?.(true)}>
      {props.embedded ? `Incident editor ${props.initial.number}` : "Not embedded"}
    </button>
  ),
}));

vi.mock("../pages/RequestsPage", () => ({
  RequestsPage: (props: { tasks: boolean; recordIDOverride: string; onDirtyChange?: (dirty: boolean) => void }) => (
    <button type="button" onClick={() => props.onDirtyChange?.(true)}>
      {props.tasks ? `Task editor ${props.recordIDOverride}` : "Not a task editor"}
    </button>
  ),
}));

afterEach(() => {
  vi.restoreAllMocks();
});

async function mount(type: "incident" | "fulfillment_task") {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const onDirtyChange = vi.fn();
  const container = document.createElement("div");
  const root: Root = createRoot(container);
  await act(async () => {
    root.render(
      <MemoryRouter>
        <WorkRecordEditor
          reference={{ tenant_id: "2", object_type: type, object_id: "31", key: `2:${type}:31` }}
          onDirtyChange={(key, dirty) => onDirtyChange(key, dirty)}
        />
      </MemoryRouter>,
    );
  });
  return { container, root, onDirtyChange };
}

it("opens the existing incident form inside its composite workspace tab", async () => {
  const { container, root, onDirtyChange } = await mount("incident");
  try {
    expect(container.textContent).toContain("Incident editor INC0031");
    await act(async () => {
      container.querySelector("button")!.click();
    });
    expect(onDirtyChange).toHaveBeenCalledWith("2:incident:31", true);
  } finally {
    await act(async () => root.unmount());
  }
});

it("opens the existing fulfillment task form inside its composite workspace tab", async () => {
  const { container, root, onDirtyChange } = await mount("fulfillment_task");
  try {
    expect(container.textContent).toContain("Task editor 31");
    await act(async () => {
      container.querySelector("button")!.click();
    });
    expect(onDirtyChange).toHaveBeenCalledWith("2:fulfillment_task:31", true);
  } finally {
    await act(async () => root.unmount());
  }
});
