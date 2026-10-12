import { afterEach, describe, expect, it, vi } from "vitest";
import {
  advanceSessionRevision,
  api,
  onSessionExpired,
  readCookie,
} from "./client";

afterEach(() => {
  document.cookie = "opensnow_csrf=; Max-Age=0; Path=/";
  vi.unstubAllGlobals();
});

describe("api client", () => {
  it("expires the shared session on protected 401, but not on a failed login", async () => {
    const expired = vi.fn();
    const unsubscribe = onSessionExpired(expired);
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              error: {
                code: "UNAUTHENTICATED",
                message: "Authentication required",
              },
            }),
            { status: 401, headers: { "Content-Type": "application/json" } },
          ),
      ),
    );
    try {
      await expect(
        api("/api/v1/auth/login", { method: "POST" }),
      ).rejects.toMatchObject({ status: 401 });
      expect(expired).not.toHaveBeenCalled();
      await expect(api("/api/v1/admin/users/")).rejects.toMatchObject({
        status: 401,
      });
      expect(expired).toHaveBeenCalledOnce();
    } finally {
      unsubscribe();
    }
  });
  it.each(["previous login", "cancelled request"])(
    "ignores a late 401 from a %s",
    async (reason) => {
      let finish!: (response: Response) => void;
      const expired = vi.fn();
      const unsubscribe = onSessionExpired(expired);
      vi.stubGlobal(
        "fetch",
        vi.fn(
          () =>
            new Promise<Response>((resolve) => {
              finish = resolve;
            }),
        ),
      );
      try {
        const controller = new AbortController();
        const pending = api("/api/v1/admin/users/", {
          signal: controller.signal,
        });
        if (reason === "previous login") advanceSessionRevision();
        else controller.abort();
        finish(new Response(null, { status: 401 }));
        await expect(pending).rejects.toMatchObject({ name: "AbortError" });
        expect(expired).not.toHaveBeenCalled();
      } finally {
        unsubscribe();
      }
    },
  );
  it("reads encoded cookies", () => {
    document.cookie = "opensnow_csrf=abc%20123; Path=/";
    expect(readCookie("opensnow_csrf")).toBe("abc 123");
  });

  it("adds CSRF header to unsafe requests", async () => {
    document.cookie = "opensnow_csrf=csrf-token; Path=/";
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, init?: RequestInit) => {
        const headers = new Headers(init?.headers);
        expect(headers.get("X-CSRF-Token")).toBe("csrf-token");
        expect(init?.credentials).toBe("include");
        return new Response(null, { status: 204 });
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    await api<void>("/api/v1/auth/logout", { method: "POST" });
    expect(fetchMock).toHaveBeenCalledOnce();
  });

  it("maps JSON errors to ApiError", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              error: {
                code: "FORBIDDEN",
                message: "Permission denied",
                request_id: "req-1",
              },
            }),
            {
              status: 403,
              headers: { "Content-Type": "application/json" },
            },
          ),
      ),
    );

    await expect(api("/api/v1/admin/rbac/roles")).rejects.toEqual(
      expect.objectContaining({
        status: 403,
        code: "FORBIDDEN",
        requestId: "req-1",
      }),
    );
  });
});
