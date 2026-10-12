import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../api/client";

type Page<T> = { items: T[]; next?: string };
type State<T> = Page<T> & {
  key: string;
  loading: boolean;
  error: string | null;
};

// Hide an old page synchronously, before effects run. Identity and cancellation
// also protect pagination and refetches against delayed responses.
export function useRemotePage<T>(
  path: string,
  collection: string,
  cursor = "after_id",
  enabled = true,
) {
  const [revision, setRevision] = useState(0);
  const key = `${enabled}:${path}|${revision}`;
  const active = useRef(key);
  active.current = key;
  const sequence = useRef(0);
  const request = useRef<AbortController | null>(null);
  const [state, setState] = useState<State<T>>({
    key: "",
    items: [],
    loading: true,
    error: null,
  });
  const load = useCallback(
    async (after?: string) => {
      if (!enabled) return;
      request.current?.abort();
      const controller = new AbortController();
      request.current = controller;
      const seq = ++sequence.current;
      setState((previous) => ({
        key,
        items: after && previous.key === key ? previous.items : [],
        loading: true,
        error: null,
      }));
      try {
        const url = new URL(path, window.location.origin);
        if (after) url.searchParams.set(cursor, after);
        const result = await api<Record<string, T[] | string | undefined>>(
          `${url.pathname}${url.search}`,
          { signal: controller.signal },
        );
        if (
          controller.signal.aborted ||
          active.current !== key ||
          seq !== sequence.current
        )
          return;
        const items = result[collection] as T[];
        setState((previous) => ({
          key,
          items: after ? [...previous.items, ...items] : items,
          next: result[`next_${cursor}`] as string | undefined,
          loading: false,
          error: null,
        }));
      } catch (cause) {
        if (
          controller.signal.aborted ||
          active.current !== key ||
          seq !== sequence.current
        )
          return;
        setState((previous) => ({
          ...previous,
          key,
          loading: false,
          error:
            cause instanceof Error
              ? cause.message
              : "Nie udało się pobrać danych.",
        }));
      }
    },
    [path, collection, cursor, key, enabled],
  );
  useEffect(() => {
    void load();
    return () => {
      request.current?.abort();
      sequence.current += 1;
    };
  }, [load]);
  const current = !enabled
    ? { items: [] as T[], loading: false, error: null, next: undefined }
    : state.key === key
      ? state
      : { items: [] as T[], loading: true, error: null, next: undefined };
  return {
    ...current,
    more: () => load(current.next),
    retry: () => load(),
    reload: () => setRevision((value) => value + 1),
  };
}
