import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { ApiError, api } from "../api/client";
import type { UserProfile } from "../api/types";
import { useAuth } from "../auth/AuthContext";
import { PageState } from "../components/PageState";

type LoadState =
  | { status: "loading"; userID: string }
  | { status: "error"; userID: string; message: string }
  | { status: "ready"; userID: string; profile: UserProfile };

function formatDate(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.valueOf()) ? "—" : date.toLocaleString("pl-PL");
}

export function UserProfilePage() {
  const { userID = "" } = useParams();
  const { session } = useAuth();
  const [revision, setRevision] = useState(0);
  const [state, setState] = useState<LoadState>({ status: "loading", userID: "" });

  useEffect(() => {
    if (!/^[1-9]\d{0,19}$/.test(userID)) {
      setState({
        status: "error",
        userID,
        message: "Nieprawidłowy identyfikator użytkownika.",
      });
      return;
    }

    const controller = new AbortController();
    setState({ status: "loading", userID });
    void api<{ profile: UserProfile }>(
      `/api/v1/admin/users/${encodeURIComponent(userID)}`,
      { signal: controller.signal },
    )
      .then(({ profile }) => {
        if (!controller.signal.aborted)
          setState({ status: "ready", userID, profile });
      })
      .catch((cause: unknown) => {
        if (
          controller.signal.aborted ||
          (cause instanceof DOMException && cause.name === "AbortError")
        )
          return;
        setState({
          status: "error",
          userID,
          message:
            cause instanceof ApiError && cause.status === 404
              ? "Nie znaleziono użytkownika w tej organizacji."
              : cause instanceof Error
                ? cause.message
                : "Nie udało się pobrać profilu użytkownika.",
        });
      });

    return () => controller.abort();
  }, [userID, revision]);

  if (state.userID !== userID || state.status === "loading")
    return (
      <section className="page">
        <PageState title="Ładowanie profilu użytkownika…" />
      </section>
    );

  if (state.status === "error")
    return (
      <section className="page">
        <div className="page-heading">
          <div>
            <p className="eyebrow">Administracja</p>
            <h1>Profil użytkownika</h1>
          </div>
          <Link className="button button--secondary" to="/users">
            Wróć do użytkowników
          </Link>
        </div>
        <PageState title="Nie można otworzyć profilu" detail={state.message} tone="error" />
        {/^[1-9]\d{0,19}$/.test(userID) ? (
          <button
            className="button button--secondary"
            onClick={() => setRevision((value) => value + 1)}
          >
            Spróbuj ponownie
          </button>
        ) : null}
      </section>
    );

  const { profile } = state;
  const isSelf = profile.id === session?.user.id;
  return (
    <section className="page">
      <div className="page-heading">
        <div>
          <p className="eyebrow">Administracja · Użytkownicy</p>
          <h1>{profile.display_name}</h1>
          <p className="muted">Profil konta w bieżącej organizacji</p>
        </div>
        <div className="action-row">
          <Link className="button button--secondary" to="/users">
            Wróć do użytkowników
          </Link>
          {isSelf ? (
            <Link className="button button--primary" to="/account">
              Edytuj mój profil
            </Link>
          ) : null}
        </div>
      </div>

      <div className="panel">
        <div className="panel-heading">
          <h2>Dane użytkownika</h2>
          <span
            className={
              profile.user_active && profile.membership_active
                ? "badge badge--success"
                : "badge"
            }
          >
            {!profile.user_active
              ? "Konto wyłączone"
              : profile.membership_active
                ? "Aktywne konto"
                : "Członkostwo wyłączone"}
          </span>
        </div>
        <dl className="detail-grid">
          <div>
            <dt>Nazwa wyświetlana</dt>
            <dd>{profile.display_name}</dd>
          </div>
          <div>
            <dt>E-mail logowania</dt>
            <dd>{profile.email}</dd>
          </div>
          <div>
            <dt>Identyfikator</dt>
            <dd>{profile.id}</dd>
          </div>
          <div>
            <dt>Status konta</dt>
            <dd>{profile.user_active ? "Aktywne" : "Wyłączone"}</dd>
          </div>
          <div>
            <dt>Członkostwo w organizacji</dt>
            <dd>{profile.membership_active ? "Aktywne" : "Wyłączone"}</dd>
          </div>
          <div>
            <dt>Utworzono konto</dt>
            <dd>{formatDate(profile.created_at)}</dd>
          </div>
          <div>
            <dt>Dołączono do organizacji</dt>
            <dd>{formatDate(profile.membership_created_at)}</dd>
          </div>
        </dl>
      </div>
    </section>
  );
}
