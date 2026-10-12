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
import {
  advanceSessionRevision,
  ApiError,
  api,
  jsonBody,
  onSessionExpired,
} from "../api/client";
import type { AuthMe, LoginRequest, SetupStatus } from "../api/types";

type AuthStatus =
  | "loading"
  | "setup_required"
  | "authenticated"
  | "anonymous"
  | "error";
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
  const currentStatus = useRef(status);
  currentStatus.current = status;
  const epoch = useRef(0);
  const inFlight = useRef<{
    controller: AbortController;
    promise: Promise<void>;
  } | null>(null);
  const clear = useCallback(() => {
    epoch.current += 1;
    inFlight.current?.controller.abort();
    inFlight.current = null;
    setSession(null);
    setStatus("anonymous");
    setError(null);
  }, []);
  useEffect(
    () =>
      onSessionExpired(() => {
        const expired = currentStatus.current === "authenticated";
        clear();
        if (expired)
          setError(
            "Sesja wygasła lub została wylogowana przez serwer. Zaloguj się ponownie.",
          );
      }),
    [clear],
  );

  const refresh = useCallback(() => {
    if (inFlight.current) return inFlight.current.promise;
    const currentEpoch = epoch.current;
    const controller = new AbortController();
    const pending = (async () => {
      try {
        const installation = await api<SetupStatus>("/api/v1/setup", {
          signal: controller.signal,
        });
        if (currentEpoch !== epoch.current || controller.signal.aborted) return;
        setSetup(installation);
        if (installation.required) {
          setSession(null);
          setStatus("setup_required");
          setError(null);
          return;
        }
        const current = await api<AuthMe>("/api/v1/auth/me", {
          signal: controller.signal,
        });
        if (currentEpoch !== epoch.current || controller.signal.aborted) return;
        setSession(current);
        setStatus("authenticated");
        setError(null);
      } catch (cause) {
        if (currentEpoch !== epoch.current || controller.signal.aborted) return;
        if (cause instanceof ApiError && cause.status === 401) {
          clear();
          return;
        }
        setSession(null);
        setStatus("error");
        setError(
          cause instanceof Error
            ? cause.message
            : "Nie udało się odczytać sesji.",
        );
        throw cause;
      } finally {
        if (inFlight.current?.controller === controller)
          inFlight.current = null;
      }
    })();
    inFlight.current = { controller, promise: pending };
    return pending;
  }, [clear]);
  useEffect(() => {
    void refresh().catch(() => undefined);
    return () => {
      inFlight.current?.controller.abort();
      inFlight.current = null;
    };
  }, [refresh]);

  // Check on returning to the app and at the server-provided expiry, without polling.
  useEffect(() => {
    if (!session) return;
    const check = () => {
      if (document.visibilityState === "visible")
        void refresh().catch(() => undefined);
    };
    window.addEventListener("focus", check);
    document.addEventListener("visibilitychange", check);
    const timer = window.setTimeout(
      () => void refresh().catch(() => undefined),
      Math.max(
        0,
        Math.min(
          2_147_483_647,
          Date.parse(session.expires_at) - Date.now() + 100,
        ),
      ),
    );
    return () => {
      clearTimeout(timer);
      window.removeEventListener("focus", check);
      document.removeEventListener("visibilitychange", check);
    };
  }, [session, refresh]);

  const login = useCallback(
    async (input: LoginRequest) => {
      advanceSessionRevision();
      epoch.current += 1;
      inFlight.current?.controller.abort();
      inFlight.current = null;
      await api("/api/v1/auth/login", {
        method: "POST",
        body: jsonBody(input),
      });
      advanceSessionRevision();
      await refresh();
    },
    [refresh],
  );
  const logout = useCallback(async () => {
    try {
      await api<void>("/api/v1/auth/logout", { method: "POST" });
    } catch (cause) {
      if (!(cause instanceof ApiError) || cause.status !== 401) throw cause;
    }
    advanceSessionRevision();
    clear();
  }, [clear]);
  const hasPermission = useCallback(
    (permission: string) => session?.permissions.includes(permission) ?? false,
    [session],
  );
  const value = useMemo(
    () => ({
      status,
      session,
      setup,
      error,
      login,
      logout,
      refresh,
      hasPermission,
    }),
    [status, session, setup, error, login, logout, refresh, hasPermission],
  );
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
export function useAuth(): AuthContextValue {
  const value = useContext(AuthContext);
  if (!value) throw new Error("useAuth must be used inside AuthProvider");
  return value;
}
