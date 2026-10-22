import { useEffect, useRef, useState } from "react";
import { api, ApiError, currentSessionRevision, jsonBody, onSessionRevisionChange } from "../api/client";
import { message } from "./contracts";
type Proposal = { key: string; body: string; path: string; scope: string };

// A keyed creation must survive the editor component being unmounted while its
// request is in flight. Keeping the proposal in this module preserves it across
// in-app navigation without writing article content to browser storage.
const pendingCreations = new Map<string, Proposal>();
onSessionRevisionChange(() => pendingCreations.clear());

export function useCreation<T>(scope?: string) {
  const [pending, setPending] = useState<Proposal | undefined>(() => scope ? pendingCreations.get(`${currentSessionRevision()}:${scope}`) : undefined);
  const proposal = useRef<Proposal | undefined>(pending),
    alive = useRef(true),
    controller = useRef<AbortController | undefined>(undefined);
  const [busy, setBusy] = useState(false),
    [uncertain, setUncertain] = useState(Boolean(pending)),
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
    if (proposal.current && (proposal.current.body !== body || proposal.current.path !== path)) {
      setUncertain(true);
      setError("Poprzednie utworzenie nie zostało uzgodnione. Przywróć jego treść i ponów zapis przed rozpoczęciem kolejnego.");
      return;
    }
    if (!proposal.current) {
      const proposalScope = `${currentSessionRevision()}:${scope ?? path}`;
      proposal.current = { key: crypto.randomUUID(), body, path, scope: proposalScope };
      pendingCreations.set(proposalScope, proposal.current);
      setPending(proposal.current);
    }
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
      pendingCreations.delete(attempt.scope);
      proposal.current = undefined;
      setPending(undefined);
      return result;
    } catch (e) {
      if (alive.current && !c.signal.aborted) {
        setError(message(e));
        setUncertain(!(e instanceof ApiError) || e.status >= 500);
        if (e instanceof ApiError && e.status < 500) {
          pendingCreations.delete(attempt.scope);
          proposal.current = undefined;
          setPending(undefined);
        }
      }
      return;
    } finally {
      if (alive.current) setBusy(false);
    }
  }
  return { create, busy, uncertain, error, pending };
}
