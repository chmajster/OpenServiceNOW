import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type PropsWithChildren,
} from "react";
import { api } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { useLocation } from "react-router-dom";
import { type Reference, referenceState } from "./references";

export type WorkRecord = {
  key: string;
  url: string;
  number: string;
  title: string;
  state: string;
  type: string;
  assignment?: string;
  links?: { url: string; label: string }[];
};
type Workspace = {
  records: WorkRecord[];
  history: WorkRecord[];
  favorites: string[];
  denied: string[];
  register: (record: WorkRecord) => void;
  remove: (key: string) => void;
  favorite: (key: string) => void;
  listURL: (root: string) => string;
};
const Context = createContext<Workspace | null>(null);
export function WorkspaceProvider({ children }: PropsWithChildren) {
  const location = useLocation();
  const { session } = useAuth();
  const [records, setRecords] = useState<WorkRecord[]>([]);
  const [history, setHistory] = useState<WorkRecord[]>([]);
  const [favorites, setFavorites] = useState<string[]>([]);
  const [denied, setDenied] = useState<string[]>([]);
  const favoritesRef = useRef(favorites);
  favoritesRef.current = favorites;
  const known = useRef<WorkRecord[]>([]);
  known.current = [
    ...new Map([...history, ...records].map((r) => [r.key, r])).values(),
  ];
  useEffect(() => {
    const controller = new AbortController();
    const keys = known.current
      .map((r) => r.key)
      .filter((key) =>
        /^\/(incidents|requests|fulfillment|problems|changes|cmdb|catalog|approvals)\/[1-9]\d*$/.test(
          key,
        ),
      );
    const batches = Array.from(
      { length: Math.ceil(keys.length / 150) },
      (_, i) => keys.slice(i * 150, (i + 1) * 150),
    );
    void Promise.allSettled(
      batches.map(async (batch) => {
        const params = new URLSearchParams();
        batch.forEach((key) => params.append("key", key));
        const result = await api<{
          items: { key: string; record: Reference }[];
        }>(`/api/v1/workspace/metadata?${params}`, {
          signal: controller.signal,
        });
        if (controller.signal.aborted) return;
        const allowed = new Map(
          result.items.map((item) => [item.key, item.record]),
        );
        const removed = new Set(batch.filter((key) => !allowed.has(key)));
        const update = (items: WorkRecord[]) =>
          items
            .filter((r) => !removed.has(r.key))
            .map((r) => {
              const current = allowed.get(r.key);
              return current
                ? {
                    ...r,
                    number: current.number,
                    title: current.title,
                    state: referenceState(current),
                  }
                : r;
            });
        setRecords(update);
        setHistory(update);
        setFavorites((items) => items.filter((key) => !removed.has(key)));
        setDenied((items) => [...new Set([...items, ...removed])].slice(-250));
      }),
    );
    return () => controller.abort();
  }, [session]);
  const lists = useRef(new Map<string, string>());
  const register = useCallback((record: WorkRecord) => {
    setDenied((items) =>
      items.includes(record.key)
        ? items.filter((key) => key !== record.key)
        : items,
    );
    const update = (items: WorkRecord[]) => {
      const found = items.find((r) => r.key === record.key);
      if (found && JSON.stringify(found) === JSON.stringify(record))
        return items;
      return [...items.filter((r) => r.key !== record.key), record];
    };
    setRecords((items) => update(items).slice(-100));
    setHistory((items) => {
      const next = [...items.filter((r) => r.key !== record.key), record];
      return next.filter(
        (r, i) =>
          i >= next.length - 100 || favoritesRef.current.includes(r.key),
      );
    });
  }, []);
  useEffect(() => {
    if (
      /^\/(incidents|requests|fulfillment|catalog|approvals|cmdb|problems|changes|cab|change-calendar|change-templates)$/.test(
        location.pathname,
      )
    )
      lists.current.set(location.pathname, location.pathname + location.search);
  }, [location.pathname, location.search]);
  const value = useMemo(
    () => ({
      records,
      history,
      favorites,
      denied,
      register,
      remove: (key: string) =>
        setRecords((items) => items.filter((r) => r.key !== key)),
      favorite: (key: string) =>
        setFavorites((items) =>
          items.includes(key)
            ? items.filter((v) => v !== key)
            : [...items, key].slice(-50),
        ),
      listURL: (root: string) => lists.current.get(root) ?? root,
    }),
    [records, history, favorites, denied, register],
  );
  return <Context.Provider value={value}>{children}</Context.Provider>;
}
export function useWorkspace() {
  return useContext(Context);
}
// Called only after a domain-authorized record read. Sensitive drafts never enter this context.
export function useWorkspaceRecord(
  record: Omit<WorkRecord, "key" | "url">,
  id: string,
) {
  const workspace = useWorkspace();
  const location = useLocation();
  const root = location.pathname.split("/")[1];
  const encoded = JSON.stringify(record);
  const register = workspace?.register;
  useEffect(() => {
    register?.({
      ...(JSON.parse(encoded) as typeof record),
      key: `/${root}/${id}`,
      url: location.pathname + location.search,
    });
  }, [register, encoded, root, id, location.pathname, location.search]);
}
