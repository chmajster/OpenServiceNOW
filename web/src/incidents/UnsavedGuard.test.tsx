import { act, useState } from "react";
import { createRoot } from "react-dom/client";
import { createMemoryRouter, RouterProvider, useNavigate, useLocation } from "react-router-dom";
import { expect, it, vi } from "vitest";
import { UnsavedGuard } from "./UnsavedGuard";

it("holds navigation during an uncertain save and retries before leaving", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  function Editor() {
    const navigate = useNavigate();
    const location = useLocation();
    const [locked, setLocked] = useState(true);
    const [dirty, setDirty] = useState(true);
    return <div>
      <UnsavedGuard dirty={dirty} locked={locked} onRetry={() => { setLocked(false); setDirty(false); }} />
      <button type="button" onClick={() => navigate("/other")}>Nawiguj</button>
      <span>{location.pathname}</span>
    </div>;
  }
  const router = createMemoryRouter([
    { path: "/edit", element: <Editor /> },
    { path: "/other", element: <p>Inny ekran</p> },
  ], { initialEntries: ["/edit"] });
  const container = document.createElement("div");
  const root = createRoot(container);
  try {
    await act(async () => root.render(<RouterProvider router={router} />));
    await act(async () => { container.querySelector("button")?.click(); });
    expect(container.textContent).toContain("Trwa uzgadnianie zapisu");
    expect(container.textContent).not.toContain("Opuść bez zapisu");
    await act(async () => {
      Array.from(container.querySelectorAll("button")).find((button) => button.textContent === "Ponów zapis")?.click();
    });
    expect(container.textContent).toContain("/edit");
    expect(container.textContent).not.toContain("Trwa uzgadnianie zapisu");
    await act(async () => { container.querySelector("button")?.click(); });
    expect(container.textContent).toContain("Inny ekran");
  } finally {
    await act(async () => root.unmount());
  }
});
