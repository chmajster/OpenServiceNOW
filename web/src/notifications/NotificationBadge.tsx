import { useEffect, useState } from "react";
import { Link, useLocation } from "react-router-dom";
import { api } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import type { NotificationPage } from "./contracts";

// Reads on navigation, focus and an explicit successful notification operation;
// no unbounded polling and no stale count after a changed login/organization.
export function NotificationBadge() {
  const { session } = useAuth();
  const location = useLocation();
  const [count, setCount] = useState<{ key: string; value: string }>();
  const key = `${session?.tenant.id}:${session?.user.id}:${session?.expires_at}:${session?.permissions.join(",")}:${location.key}`;
  useEffect(() => {
    let controller: AbortController | undefined;
    const load = () => {
      controller?.abort();
      controller = new AbortController();
      const current = controller;
      void api<NotificationPage>("/api/v1/notifications/?limit=1", {
        signal: current.signal,
      })
        .then((data) => {
          if (!current.signal.aborted) setCount({ key, value: data.unread });
        })
        .catch(() => {
          if (!current.signal.aborted) setCount(undefined);
        });
    };
    load();
    window.addEventListener("focus", load);
    window.addEventListener("notifications-changed", load);
    return () => {
      controller?.abort();
      window.removeEventListener("focus", load);
      window.removeEventListener("notifications-changed", load);
    };
  }, [key]);
  const unread = count?.key === key ? count.value : undefined;
  return (
    <Link
      className="button button--small button--secondary topbar-notifications"
      to="/notifications"
      aria-label={
        unread
          ? "Powiadomienia. Nieprzeczytane: " + unread
          : "Powiadomienia"
      }
    >
      <svg
        aria-hidden="true"
        focusable="false"
        fill="none"
        stroke="currentColor"
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeWidth="1.7"
        viewBox="0 0 24 24"
      >
        <path d="M18 9a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9M10 21h4" />
      </svg>
      <span className="notification-label">
        Powiadomienia{unread ? ` (${unread})` : ""}
      </span>
    </Link>
  );
}
