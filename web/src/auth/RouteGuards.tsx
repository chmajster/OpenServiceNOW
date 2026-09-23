import type { PropsWithChildren } from "react";
import { Navigate } from "react-router-dom";
import { useAuth } from "./AuthContext";
import { PageState } from "../components/PageState";

export function RequireAuth({ children }: PropsWithChildren) {
  const { status, error } = useAuth();

  if (status === "loading") {
    return <PageState title="Ładowanie sesji" detail="Sprawdzanie aktywnej sesji użytkownika." />;
  }
  if (status === "error") {
    return <PageState title="Błąd sesji" detail={error ?? "Nie udało się odczytać sesji."} tone="error" />;
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
