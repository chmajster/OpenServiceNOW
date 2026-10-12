import { useEffect, useRef, useState } from "react";
import { api, ApiError, jsonBody } from "../api/client";
import { message } from "./contracts";
export function useCreation<T>() {
  const proposal = useRef<
      { key: string; body: string; path: string } | undefined
    >(undefined),
    alive = useRef(true),
    controller = useRef<AbortController | undefined>(undefined);
  const [busy, setBusy] = useState(false),
    [uncertain, setUncertain] = useState(false),
    [error, setError] = useState<string>();
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      controller.current?.abort();
    };
  }, []);
  async function create(path: string, input: unknown) {
    if (busy) return;
    const body = jsonBody(input);
    if (
      !uncertain &&
      (!proposal.current ||
        proposal.current.body !== body ||
        proposal.current.path !== path)
    )
      proposal.current = { key: crypto.randomUUID(), body, path };
    const attempt = proposal.current!;
    setBusy(true);
    setError(undefined);
    const c = new AbortController();
    controller.current = c;
    try {
      const result = await api<T>(attempt.path, {
        method: "POST",
        headers: { "Idempotency-Key": attempt.key },
        body: attempt.body,
        signal: c.signal,
      });
      if (!alive.current || c.signal.aborted) return;
      setUncertain(false);
      proposal.current = undefined;
      return result;
    } catch (e) {
      if (alive.current && !c.signal.aborted) {
        setError(message(e));
        setUncertain(!(e instanceof ApiError) || e.status >= 500);
        if (e instanceof ApiError && e.status < 500)
          proposal.current = undefined;
      }
      return;
    } finally {
      if (alive.current) setBusy(false);
    }
  }
  return { create, busy, uncertain, error };
}
