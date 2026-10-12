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
  const key = `${session?.tenant.id}:${session?.user.id}:${session?.expires_at}:${location.key}`;
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
  return (
    <Link
      className="button button--small button--secondary"
      to="/notifications"
    >
      Powiadomienia{count?.key === key ? ` (${count.value})` : ""}
    </Link>
  );
}
