import { useEffect, useRef, useState } from "react";

import { Link } from "react-router-dom";

import { api, ApiError } from "../api/client";

import { useAuth } from "../auth/AuthContext";

import { useCMDBLocale } from "../cmdb/messages";

import { RemoteCombobox, type Choice } from "../components/RemoteCombobox";

import { useIncidentQuery } from "./hooks";

import { UnsavedGuard } from "./UnsavedGuard";

import { formatDateTime } from "../metadata/format";

import { MajorActionList } from "./MajorPanel";

import { majorMessages } from "./majorMessages";

import type { MajorAction } from "./majorContracts";

export function CoordinationActionEditor({
  id,
  onDirtyChange,
}: {
  id: string;
  onDirtyChange?: (dirty: boolean) => void;
}) {
  const locale = useCMDBLocale(),
    t = majorMessages[locale],
    { hasPermission, session } = useAuth();

  const item = useIncidentQuery<MajorAction>(
    `/api/v1/incidents/major/actions/${id}`,
  );

  type Note = {
    id: string;
    body: string;
    author_name: string;
    created_at: string;
  };

  const notes = useIncidentQuery<{ messages: Note[]; next_before_id?: string }>(
    `/api/v1/incidents/major/actions/${id}/messages`,
  );

  const [body, setBody] = useState(""),
    [older, setOlder] = useState<Note[]>([]),
    [cursor, setCursor] = useState<string>();

  const [conflict, setConflict] = useState(false);
  const assignmentVersion = useRef<string | undefined>(undefined);
  const receipt = useRef<{ body: string; version: string; key: string } | null>(
    null,
  );

  async function addNote() {
    if (!item.data || busy || !body.trim()) return;

    const request =
      receipt.current?.body === body
        ? receipt.current
        : { body, version: item.data.version, key: crypto.randomUUID() };
    receipt.current = request;

    setBusy(true);
    setError(undefined);

    try {
      await api(`/api/v1/incidents/major/actions/${id}/messages`, {
        method: "POST",
        headers: { "Idempotency-Key": request.key },
        body: JSON.stringify({ body: request.body, version: request.version }),
      });
      setBody("");
      receipt.current = null;
      setOlder([]);
      setCursor(undefined);
      notes.reload();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t.error);
      if (cause instanceof ApiError && cause.status === 409) {
        setConflict(true);
        item.reload();
      }
    } finally {
      setBusy(false);
    }
  }

  async function more() {
    const before = cursor ?? notes.data?.next_before_id;
    if (!before) return;
    setBusy(true);
    try {
      const page = await api<{ messages: Note[]; next_before_id?: string }>(
        `/api/v1/incidents/major/actions/${id}/messages?before_id=${before}`,
      );
      setOlder((old) => [...old, ...page.messages]);
      setCursor(page.next_before_id ?? "0");
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t.error);
      if (cause instanceof ApiError && cause.status === 409) {
        setConflict(true);
        item.reload();
      }
    } finally {
      setBusy(false);
    }
  }

  const [busy, setBusy] = useState(false),
    [error, setError] = useState<string>(),
    [owner, setOwner] = useState<Choice | null>(null);

  async function change(status: string) {
    if (!item.data || busy) return;

    setBusy(true);

    setError(undefined);

    try {
      await api(`/api/v1/incidents/major/actions/${id}`, {
        method: "PUT",

        body: JSON.stringify({ version: item.data.version, status }),
      });

      item.reload();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t.error);
      if (cause instanceof ApiError && cause.status === 409) {
        setConflict(true);
        item.reload();
      }
    } finally {
      setBusy(false);
    }
  }

  async function assign() {
    if (!item.data || !owner || busy) return;

    setBusy(true);

    setError(undefined);

    try {
      const result = await api<{
        results: { status: string; message?: string }[];
      }>("/api/v1/workspace/actions", {
        method: "POST",

        body: JSON.stringify({
          action: "assign",

          assigned_to: owner.id,

          items: [
            {
              tenant_id: session?.tenant.id,

              object_type: "incident_action",

              object_id: id,

              version: assignmentVersion.current ?? item.data.version,
            },
          ],
        }),
      });

      if (result.results[0]?.status !== "SUCCEEDED")
        throw new Error(result.results[0]?.message || t.error);

      setOwner(null);
      assignmentVersion.current = undefined;

      item.reload();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t.error);
      if (cause instanceof ApiError && cause.status === 409) {
        setConflict(true);
        item.reload();
      }
    } finally {
      setBusy(false);
    }
  }

  useEffect(() => {
    onDirtyChange?.(Boolean(body.trim() || owner));
  }, [body, owner, onDirtyChange]);
  useEffect(() => () => onDirtyChange?.(false), [onDirtyChange]);
  return (
    <section>
      {!onDirtyChange && (
        <UnsavedGuard dirty={Boolean(body.trim() || owner)} includeSearch />
      )}
      {conflict && (
        <section className="notice">
          <p>{t.conflict}</p>
          <p>
            {t.version}: {item.data?.version}
          </p>
          <button
            className="button button--secondary"
            disabled={item.loading}
            onClick={() => {
              receipt.current = null;
              assignmentVersion.current = item.data?.version;
              setConflict(false);
              setError(undefined);
            }}
          >
            {t.rebaseDraft}
          </button>
        </section>
      )}

      <h2>{t.technicalActions}</h2>

      {item.loading && <p role="status">{t.loading}</p>}

      {(error || item.error) && (
        <p role="alert" className="notice notice--error">
          {error || item.error}
        </p>
      )}

      {item.data && (
        <>
          <Link to={`/incidents/${item.data.incident_id}?tab=major`}>
            {t.coordination}
          </Link>

          <MajorActionList
            items={[item.data]}
            busy={busy}
            change={(_, state) => void change(state)}
          />

          {hasPermission("incident.major.manage") && (
            <fieldset disabled={busy} className="form-fields">
              <RemoteCombobox
                kind="users"
                label={t.owner}
                endpoint="/api/v1/incidents/lookups/users?purpose=coordination"
                value={owner}
                onChange={(value) => {
                  if (!owner) assignmentVersion.current = item.data?.version;
                  setOwner(value);
                }}
              />

              <button
                className="button button--secondary"
                disabled={!owner}
                onClick={() => void assign()}
              >
                {t.save}
              </button>
            </fieldset>
          )}
        </>
      )}

      <section>
        <h3>{t.notes}</h3>
        {notes.loading && <p role="status">{t.loading}</p>}
        {notes.error && <p role="alert">{notes.error}</p>}

        {[...(notes.data?.messages ?? []), ...older].map((note) => (
          <article key={note.id}>
            <strong>{note.author_name}</strong> ·{" "}
            <time>{formatDateTime(note.created_at, locale)}</time>
            <p className="preserve-lines">{note.body}</p>
          </article>
        ))}

        {!notes.loading && !notes.data?.messages.length && <p>{t.empty}</p>}

        {(cursor ?? notes.data?.next_before_id) &&
          (cursor ?? notes.data?.next_before_id) !== "0" && (
            <button
              className="button button--secondary"
              disabled={busy}
              onClick={() => void more()}
            >
              {t.more}
            </button>
          )}

        {(hasPermission("incident.major.manage") ||
          item.data?.owner_user_id === session?.user.id) && (
          <form
            onSubmit={(e) => {
              e.preventDefault();
              void addNote();
            }}
          >
            <label>
              {t.notes}
              <textarea
                required
                maxLength={20000}
                value={body}
                onChange={(e) => {
                  setBody(e.target.value);
                  receipt.current = null;
                }}
              />
            </label>
            <button
              className="button button--secondary"
              disabled={busy || !body.trim()}
            >
              {t.addNote}
            </button>
          </form>
        )}
      </section>

      <button
        className="button button--secondary"
        disabled={busy}
        onClick={() => {
          item.reload();
          setOlder([]);
          setCursor(undefined);
          notes.reload();
        }}
      >
        {t.refresh}
      </button>
    </section>
  );
}
