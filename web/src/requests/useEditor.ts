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
    [conflict, setConflict] = useState<T>();
  const alive = useRef(true),
    controller = useRef<AbortController | undefined>(undefined);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      controller.current?.abort();
    };
  }, []);
  async function save(path: string, body: unknown, method = "POST") {
    if (busy || conflict) return false;
    setBusy(true);
    setError(undefined);
    const c = new AbortController();
    controller.current = c;
    try {
      const saved = await api<T>(path, {
        method,
        body: jsonBody(body),
        signal: c.signal,
      });
      if (alive.current) {
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
