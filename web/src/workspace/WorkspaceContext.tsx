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
import { api, ApiError } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { useLocation } from "react-router-dom";

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
  const favoritesRef = useRef(favorites);
  favoritesRef.current = favorites;
  const known = useRef<WorkRecord[]>([]);
  known.current = [
    ...new Map([...history, ...records].map((r) => [r.key, r])).values(),
  ];
  useEffect(() => {
    const controller = new AbortController();
    const paths: Record<string, string> = {
      incidents: "incidents",
      requests: "requests",
      fulfillment: "fulfillment",
      approvals: "workflow/executions",
    };
    void Promise.allSettled(
      known.current.map(async (record) => {
        const domain = paths[record.key.split("/")[1] ?? ""];
        if (!domain) return;
        try {
          await api(`/api/v1/${domain}/${record.key.split("/")[2]}`, {
            signal: controller.signal,
          });
        } catch (cause) {
          if (
            !controller.signal.aborted &&
            cause instanceof ApiError &&
            [401, 403, 404].includes(cause.status)
          ) {
            setRecords((items) => items.filter((r) => r.key !== record.key));
            setHistory((items) => items.filter((r) => r.key !== record.key));
            setFavorites((items) => items.filter((key) => key !== record.key));
          }
        }
      }),
    );
    return () => controller.abort();
  }, [session]);
  const lists = useRef(new Map<string, string>());
  const register = useCallback((record: WorkRecord) => {
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
      /^\/(incidents|requests|fulfillment|catalog|approvals)$/.test(
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
    [records, history, favorites, register],
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
