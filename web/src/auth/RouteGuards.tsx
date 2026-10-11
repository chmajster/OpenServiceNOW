import type { PropsWithChildren } from "react";
import { Navigate } from "react-router-dom";
import { useAuth } from "./AuthContext";
import { PageState } from "../components/PageState";

export function RequireAuth({ children }: PropsWithChildren) {
  const { status, error, refresh } = useAuth();

  if (status === "loading") {
    return <PageState title="Ładowanie sesji" detail="Sprawdzanie aktywnej sesji użytkownika." />;
  }
  if (status === "error") {
    return (
      <div className="form-stack form-stack--compact">
        <PageState title="Błąd sesji" detail={error ?? "Nie udało się odczytać sesji."} tone="error" />
        <button
          className="button button--primary"
          type="button"
          onClick={() => void refresh().catch(() => undefined)}
        >
          Spróbuj ponownie
        </button>
      </div>
    );
  }
  if (status === "setup_required") {
    return <Navigate to="/setup" replace />;
  }
  if (status !== "authenticated") {
    return <Navigate to="/login" replace />;
  }
  return children;
}

export function RequirePermission({
  permission,
  children
}: PropsWithChildren<{ permission: string }>) {
  const { hasPermission } = useAuth();
  if (!hasPermission(permission)) {
    return (
      <PageState
        title="Brak uprawnień"
        detail={`Ta sekcja wymaga uprawnienia: ${permission}`}
        tone="error"
      />
    );
  }
  return children;
}
