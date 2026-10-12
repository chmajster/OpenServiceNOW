import { useEffect, useRef, useState, type FormEvent } from "react";
import { api, ApiError, jsonBody } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import type { Incident } from "./contracts";

export function useIncidentTitleFocus(key?: string) {
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    heading.current?.focus();
  }, [key]);
  return heading;
}

export function useIncidentQuery<T>(path: string | null, scope = "") {
  const { session } = useAuth();
  const [revision, setRevision] = useState(0);
  const key = `${session?.tenant.id}:${session?.user.id}:${session?.expires_at}:${session?.permissions?.join(",")}:${path}:${scope}:${revision}`;
  const current = useRef(key);
  current.current = key;
  const [state, setState] = useState<{
    key: string;
    data?: T;
    error?: string;
    loading: boolean;
  }>({ key: "", loading: true });
  useEffect(() => {
    if (!path) {
      setState({ key, loading: false });
      return;
    }
    const controller = new AbortController();
    setState({ key, loading: true });
    void api<T>(path, { signal: controller.signal })
      .then((data) => {
        if (!controller.signal.aborted && current.current === key)
          setState({ key, data, loading: false });
      })
      .catch((cause) => {
        if (!controller.signal.aborted && current.current === key)
          setState({
            key,
            loading: false,
            error:
              cause instanceof Error
                ? cause.message
                : "Nie udało się pobrać danych.",
          });
      });
    return () => controller.abort();
  }, [key, path]);
  const result =
    state.key === key
      ? state
      : { loading: true, data: undefined, error: undefined };
  return { ...result, reload: () => setRevision((value) => value + 1) };
}

// Each draft owns its version. A fresh read never silently rebases dirty input.
export function useIncidentForm<T>(
  record: Incident,
  makeDraft: (record: Incident) => T,
  saved: (record: Incident, confirmed?: boolean) => void,
  setBusy: (value: boolean) => void,
) {
  const make = useRef(makeDraft);
  make.current = makeDraft;
  const [draft, updateDraft] = useState(() => makeDraft(record));
  const [version, setVersion] = useState(record.version);
  const [dirty, setDirty] = useState(false);
  const [error, setError] = useState<string>();
  const [conflict, setConflict] = useState<Incident | null>();
  const [uncertain, setUncertain] = useState(false);
  const mounted = useRef(true);
  const request = useRef<AbortController | null>(null);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      request.current?.abort();
    };
  }, []);
  useEffect(() => {
    if (!dirty && conflict === undefined && version !== record.version) {
      updateDraft(make.current(record));
      setVersion(record.version);
    }
  }, [record, version, dirty, conflict]);
  const setDraft = (next: T) => {
    updateDraft(next);
    setDirty(true);
  };
  async function latest(forConflict: boolean) {
    setBusy(true);
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    try {
      const current = await api<Incident>(`/api/v1/incidents/${record.id}`, {
        signal: controller.signal,
      });
      if (!mounted.current || controller.signal.aborted) return;
      saved(current, false);
      if (forConflict) setConflict(current);
      setUncertain(false);
    } catch (cause) {
      if (mounted.current && !controller.signal.aborted)
        setError(
          cause instanceof Error
            ? cause.message
            : "Nie udało się odczytać aktualnego stanu.",
        );
    } finally {
      if (mounted.current) setBusy(false);
    }
  }
  async function submit(
    event: FormEvent,
    path: string,
    method: string,
    serialize: () => object,
  ) {
    event.preventDefault();
    if (conflict !== undefined) return;
    setError(undefined);
    setUncertain(false);
    let body: object;
    try {
      body = { ...serialize(), version };
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Sprawdź dane.");
      return;
    }
    setBusy(true);
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    try {
      const result = await api<Incident>(path, {
        method,
        body: jsonBody(body),
        signal: controller.signal,
      });
      if (!mounted.current || controller.signal.aborted) return;
      saved(result);
      updateDraft(make.current(result));
      setVersion(result.version);
      setDirty(false);
    } catch (cause) {
      if (!mounted.current || controller.signal.aborted) return;
      setError(
        cause instanceof Error
          ? cause.message
          : "Nie udało się potwierdzić zapisu.",
      );
      if (cause instanceof ApiError && cause.status === 409) {
        setConflict(null);
        await latest(true);
      } else if (!(cause instanceof ApiError) || cause.status >= 500)
        setUncertain(true);
    } finally {
      if (mounted.current) setBusy(false);
    }
  }
  return {
    draft,
    setDraft,
    version,
    error,
    conflict,
    uncertain,
    dirty,
    submit,
    retryConflict: () => latest(true),
    refreshUncertain: () => latest(false),
    acceptCurrent: () => {
      if (!conflict) return;
      updateDraft(make.current(conflict));
      setVersion(conflict.version);
      setDirty(false);
      setConflict(undefined);
      setError(undefined);
    },
    keepProposal: () => {
      if (!conflict) return;
      setVersion(conflict.version);
      setConflict(undefined);
      setError(undefined);
      setDirty(true);
    },
  };
}
