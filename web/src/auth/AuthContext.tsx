import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type PropsWithChildren
} from "react";
import { ApiError, api, jsonBody } from "../api/client";
import type { AuthMe, LoginRequest, SetupStatus } from "../api/types";

type AuthStatus = "loading" | "setup_required" | "authenticated" | "anonymous" | "error";

type AuthContextValue = {
  status: AuthStatus;
  session: AuthMe | null;
  setup: SetupStatus | null;
  error: string | null;
  login: (input: LoginRequest) => Promise<void>;
  logout: () => Promise<void>;
  refresh: () => Promise<void>;
  hasPermission: (permission: string) => boolean;
};

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: PropsWithChildren) {
  const [status, setStatus] = useState<AuthStatus>("loading");
  const [session, setSession] = useState<AuthMe | null>(null);
  const [setup, setSetup] = useState<SetupStatus | null>(null);
  const [error, setError] = useState<string | null>(null);
  const refreshInFlight = useRef<Promise<void> | null>(null);

  const refresh = useCallback(() => {
    if (refreshInFlight.current) return refreshInFlight.current;
    const pending = (async () => {
      try {
        const installation = await api<SetupStatus>("/api/v1/setup");
        setSetup(installation);
        if (installation.required) {
          setSession(null);
          setStatus("setup_required");
          setError(null);
          return;
        }
        const current = await api<AuthMe>("/api/v1/auth/me");
        setSession(current);
        setStatus("authenticated");
        setError(null);
      } catch (cause) {
        if (cause instanceof ApiError && cause.status === 401) {
          setSession(null);
          setStatus("anonymous");
          setError(null);
          return;
        }
        setSession(null);
        setStatus("error");
        setError(cause instanceof Error ? cause.message : "Nie udało się odczytać sesji");
        throw cause;
      }
    })();
    refreshInFlight.current = pending;
    void pending.finally(() => {
      if (refreshInFlight.current === pending) refreshInFlight.current = null;
    }).catch(() => undefined);
    return pending;
  }, []);

  useEffect(() => {
    void refresh().catch(() => undefined);
  }, [refresh]);

  const login = useCallback(
    async (input: LoginRequest) => {
      await api("/api/v1/auth/login", {
        method: "POST",
        body: jsonBody(input)
      });
      await refresh();
    },
    [refresh]
  );

  const logout = useCallback(async () => {
    try {
      await api<void>("/api/v1/auth/logout", { method: "POST" });
    } catch (cause) {
      if (!(cause instanceof ApiError) || cause.status !== 401) {
        throw cause;
      }
    }
    setSession(null);
    setStatus("anonymous");
    setError(null);
  }, []);

  const hasPermission = useCallback(
    (permission: string) => session?.permissions.includes(permission) ?? false,
    [session]
  );

  const value = useMemo<AuthContextValue>(
    () => ({ status, session, setup, error, login, logout, refresh, hasPermission }),
    [status, session, setup, error, login, logout, refresh, hasPermission]
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const value = useContext(AuthContext);
  if (!value) {
    throw new Error("useAuth must be used inside AuthProvider");
  }
  return value;
}
