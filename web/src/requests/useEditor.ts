import { useEffect, useRef, useState } from "react";
import { api, ApiError, jsonBody } from "../api/client";
import { message } from "./contracts";
// Local proposals are not overwritten by conflict reads or late responses.
export function useEditor<T extends { version: string }>(
  initial: T,
  readPath: string,
) {
  const [record, setRecord] = useState(initial),
    [busy, setBusy] = useState(false),
    [error, setError] = useState<string>(),
    [notice, setNotice] = useState<string>(),
    [conflict, setConflict] = useState<T>(),
    [uncertain, setUncertain] = useState(false);
  const pending = useRef<
    | { path: string; body: unknown; method: string; headers?: HeadersInit }
    | undefined
  >(undefined);
  const alive = useRef(true),
    controller = useRef<AbortController | undefined>(undefined);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      controller.current?.abort();
    };
  }, []);
  async function save(
    path: string,
    body: unknown,
    method = "POST",
    headers?: HeadersInit,
  ) {
    if (busy || conflict) return false;
    if (method !== "GET") {
      if (uncertain && pending.current) {
        ({ path, body, method, headers } = pending.current);
      } else pending.current = { path, body, method, headers };
    }
    setBusy(true);
    setError(undefined);
    const c = new AbortController();
    controller.current = c;
    try {
      const saved = await api<T>(path, {
        method,
        headers,
        body: jsonBody(body),
        signal: c.signal,
      });
      if (alive.current) {
        if (method !== "GET") {
          setUncertain(false);
          pending.current = undefined;
        }
        setRecord(saved);
        setNotice(
          method === "GET"
            ? "Odświeżono aktualny stan."
            : "Zapisano. Historia zostanie uzupełniona przez worker.",
        );
      }
      return alive.current && !c.signal.aborted;
    } catch (e) {
      if (alive.current && !c.signal.aborted) {
        setError(message(e));
        if (method !== "GET") {
          setUncertain(!(e instanceof ApiError) || e.status >= 500);
          if (e instanceof ApiError && e.status < 500)
            pending.current = undefined;
        }
        if (e instanceof ApiError && e.status === 409) {
          try {
            const current = await api<T>(readPath, { signal: c.signal });
            if (alive.current) setConflict(current);
          } catch (cause) {
            if (alive.current) setError(`${message(e)} ${message(cause)}`);
          }
        }
      }
      return false;
    } finally {
      if (alive.current) setBusy(false);
    }
  }
  return {
    record,
    busy,
    error,
    notice,
    conflict,
    uncertain,
    save,
    setRecord,
    resolve: () => {
      if (conflict) {
        setRecord(conflict);
        setConflict(undefined);
        setError(undefined);
      }
    },
  };
}
