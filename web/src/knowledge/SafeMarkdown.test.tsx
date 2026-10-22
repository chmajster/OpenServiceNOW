import { act } from "react";
import { createRoot } from "react-dom/client";
import { expect, it, vi } from "vitest";
import { SafeMarkdown } from "./SafeMarkdown";

it("renders useful markdown without interpreting embedded HTML", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const container = document.createElement("div");
  const root = createRoot(container);
  try {
    await act(async () => root.render(<SafeMarkdown source={'# Tytuł\n\n<script>window.pwned=1</script>\n\n- **ważne**\n\n```\n<a href="javascript:alert(1)">x</a>\n```'} />));
    expect(container.querySelector("h1")?.textContent).toBe("Tytuł");
    expect(container.querySelector("strong")?.textContent).toBe("ważne");
    expect(container.querySelector("script")).toBeNull();
    expect(container.querySelector("a")).toBeNull();
    expect(container.textContent).toContain("<script>window.pwned=1</script>");
    expect(container.textContent).toContain('javascript:alert(1)');
  } finally {
    await act(async () => root.unmount());
    vi.unstubAllGlobals();
  }
});
