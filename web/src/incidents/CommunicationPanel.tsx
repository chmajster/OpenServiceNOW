import { useEffect, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { api, ApiError, jsonBody, upload, download } from "../api/client";
import { PageState } from "../components/PageState";
import { useIncidentQuery } from "./hooks";
import { dateLabel } from "./contracts";

export type Access = {
  reply: boolean;
  read_notes: boolean;
  write_notes: boolean;
  manage: boolean;
  max_file_bytes: number;
};
type Message = {
  id: string;
  type: "COMMENT" | "WORK_NOTE";
  body: string;
  author_user_id: string;
  author_name: string;
  created_at: string;
};
type File = {
  id: string;
  file_name: string;
  content_type: string;
  size_bytes: string;
  visibility: string;
  message_id?: string;
  uploaded_by?: string;
  created_at: string;
  can_delete: boolean;
};
const errorText = (cause: unknown) =>
  cause instanceof Error ? cause.message : "Nie potwierdzono zapisu.";

export function CommunicationPanel({
  record,
  retain,
  apiBase,
}: {
  record: { id: string };
  retain: (tab: string, needed: boolean) => void;
  apiBase?: string;
}) {
  const base = apiBase ?? `/api/v1/incidents/${record.id}`;
  const access = useIncidentQuery<Access>(`${base}/communication-access`);
  const [params, setParams] = useSearchParams();
  const before = params.get("messages_before_id") ?? "";
  const filesBefore = params.get("files_before_id") ?? "";
  const messages = useIncidentQuery<{
    messages: Message[];
    next_before_id?: string;
  }>(`${base}/messages?limit=20${before ? `&before_id=${before}` : ""}`);
  const files = useIncidentQuery<{ files: File[]; next_before_id?: string }>(
    `${base}/files?limit=20${filesBefore ? `&before_id=${filesBefore}` : ""}`,
  );
  const [kind, setKind] = useState<"COMMENT" | "WORK_NOTE">("COMMENT");
  const [drafts, setDrafts] = useState({ COMMENT: "", WORK_NOTE: "" });
  const body = drafts[kind];
  function setBody(value: string) {
    setDrafts((current) => ({ ...current, [kind]: value }));
  }
  function selectKind(next: "COMMENT" | "WORK_NOTE") {
    if (next === kind) return;
    setKind(next);
    setMessageId("");
    if (next === "WORK_NOTE") setVisibility("INTERNAL");
    else if (!file) setVisibility("PUBLIC");
  }
  const [file, setFile] = useState<globalThis.File>();
  const [visibility, setVisibility] = useState("PUBLIC");
  const [messageId, setMessageId] = useState("");
  const [busy, setBusy] = useState(false),
    [percent, setPercent] = useState<number>();
  const [error, setError] = useState<string>(),
    [notice, setNotice] = useState<string>();
  const [messageRetry, setMessageRetry] = useState<{
    key: string;
    body: string;
    type: string;
  }>();
  const [fileRetry, setFileRetry] = useState<{ key: string; form: FormData }>();
  const mounted = useRef(true),
    controller = useRef<AbortController | null>(null),
    picker = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (access.data && !access.data.reply && access.data.write_notes) {
      setKind("WORK_NOTE");
      setVisibility("INTERNAL");
    }
  }, [access.data?.reply, access.data?.write_notes]);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      controller.current?.abort();
    };
  }, []);
  useEffect(() => {
    retain(
      "communication",
      !!drafts.COMMENT ||
        !!drafts.WORK_NOTE ||
        !!file ||
        !!messageRetry ||
        !!fileRetry ||
        busy,
    );
  }, [drafts, file, messageRetry, fileRetry, busy, retain]);
  function page(key: string, value?: string) {
    const next = new URLSearchParams(params);
    if (value) next.set(key, value);
    else next.delete(key);
    setParams(next);
  }
  const resetReads = () => {
    messages.reload();
    files.reload();
  };
  async function send() {
    if (
      busy ||
      fileRetry ||
      !access.data ||
      !(kind === "COMMENT" ? access.data.reply : access.data.write_notes)
    )
      return;
    const proposal = messageRetry ?? {
      key: crypto.randomUUID(),
      body: body.trim(),
      type: kind,
    };
    if (!proposal.body || Array.from(proposal.body).length > 20000) {
      setError("Wiadomość wymaga 1–20000 znaków.");
      return;
    }
    setBusy(true);
    setError(undefined);
    setNotice(undefined);
    controller.current = new AbortController();
    try {
      const result = await api<Message>(`${base}/messages`, {
        method: "POST",
        headers: { "Idempotency-Key": proposal.key },
        body: jsonBody({ body: proposal.body, type: proposal.type }),
        signal: controller.current.signal,
      });
      if (!mounted.current) return;
      setBody("");
      setMessageRetry(undefined);
      setMessageId(result.id);
      setVisibility(result.type === "WORK_NOTE" ? "INTERNAL" : "PUBLIC");
      setNotice(
        "Wiadomość zapisana. Oś aktywności może jeszcze oczekiwać na worker.",
      );
      messages.reload();
    } catch (cause) {
      if (mounted.current) {
        setError(errorText(cause));
        if (!(cause instanceof ApiError) || cause.status >= 500)
          setMessageRetry(proposal);
      }
    } finally {
      if (mounted.current) setBusy(false);
    }
  }
  async function sendFile() {
    if (busy || !file || !access.data) return;
    if (file.size === 0 || file.size > access.data.max_file_bytes) {
      setError("Wybierz niepusty plik o rozmiarze do 10 MiB.");
      return;
    }
    const form = new FormData();
    form.append("file", file);
    form.append("visibility", visibility);
    if (messageId) form.append("message_id", messageId);
    const proposal = fileRetry ?? { key: crypto.randomUUID(), form };
    setBusy(true);
    setError(undefined);
    setNotice(undefined);
    setPercent(0);
    controller.current = new AbortController();
    try {
      await upload<File>(
        `${base}/files`,
        proposal.form,
        proposal.key,
        (value) => {
          if (mounted.current) setPercent(value);
        },
        controller.current.signal,
      );
      if (!mounted.current) return;
      setFile(undefined);
      if (picker.current) picker.current.value = "";
      setFileRetry(undefined);
      setPercent(undefined);
      setNotice(
        "Plik zapisany. Jest dostępny na liście; projekcja aktywności może być opóźniona.",
      );
      files.reload();
    } catch (cause) {
      if (mounted.current) {
        setError(errorText(cause));
        if (!(cause instanceof ApiError) || cause.status >= 500)
          setFileRetry(proposal);
      }
    } finally {
      if (mounted.current) setBusy(false);
    }
  }
  async function fileAction(f: File, remove: boolean) {
    if (remove && !window.confirm(`Usunąć plik „${f.file_name}”?`)) return;
    setBusy(true);
    setError(undefined);
    controller.current = new AbortController();
    try {
      if (remove) {
        await api(`${base}/files/${f.id}`, {
          method: "DELETE",
          signal: controller.current.signal,
        });
        if (mounted.current) {
          files.reload();
          setNotice("Plik usunięty z incydentu.");
        }
      } else
        await download(
          `${base}/files/${f.id}`,
          f.file_name,
          controller.current.signal,
        );
    } catch (cause) {
      if (mounted.current) setError(errorText(cause));
    } finally {
      if (mounted.current) setBusy(false);
    }
  }
  return (
    <>
      <h2>Komunikacja i pliki</h2>
      {access.loading ? (
        <PageState title="Sprawdzanie dostępu…" />
      ) : access.error ? (
        <PageState
          title="Nie udało się sprawdzić dostępu"
          detail={access.error}
          tone="error"
        />
      ) : null}
      {error && (
        <p className="notice notice--error" role="alert">
          {error}
        </p>
      )}
      {notice && (
        <p className="notice" role="status">
          {notice}
        </p>
      )}
      {(access.data?.reply || access.data?.write_notes) && (
        <form
          onSubmit={(event) => {
            event.preventDefault();
            void send();
          }}
        >
          <div
            className="action-row"
            role="group"
            aria-label="Rodzaj komunikacji"
          >
            {access.data.reply && (
              <button
                type="button"
                className="button button--secondary"
                aria-pressed={kind === "COMMENT"}
                disabled={busy || !!messageRetry || !!fileRetry}
                onClick={() => selectKind("COMMENT")}
              >
                Odpowiedź dla zgłaszającego
              </button>
            )}
            {access.data.write_notes && (
              <button
                type="button"
                className="button button--secondary"
                aria-pressed={kind === "WORK_NOTE"}
                disabled={busy || !!messageRetry || !!fileRetry}
                onClick={() => selectKind("WORK_NOTE")}
              >
                Notatka wewnętrzna
              </button>
            )}
          </div>
          <label>
            {kind === "WORK_NOTE"
              ? "Treść notatki wewnętrznej"
              : "Treść odpowiedzi"}
            <textarea
              aria-label={
                kind === "WORK_NOTE"
                  ? "Treść notatki wewnętrznej"
                  : "Treść odpowiedzi"
              }
              rows={5}
              value={body}
              disabled={busy || !!messageRetry || !!fileRetry}
              onChange={(event) => {
                setBody(event.target.value);
                setMessageId("");
              }}
            />
          </label>
          <p className="muted">
            {kind === "WORK_NOTE"
              ? "Tę treść odczytają wyłącznie operatorzy z uprawnieniem do notatek."
              : "Ta treść jest dostępna dla zgłaszającego i osób mających dostęp do incydentu."}
          </p>
          {messageRetry && (
            <p className="notice">
              Wynik poprzedniej próby jest nieznany. Ponowienie użyje zachowanej
              treści i klucza.
            </p>
          )}
          <button
            className="button button--primary"
            disabled={busy || !!fileRetry || !body.trim()}
          >
            {messageRetry
              ? "Sprawdź zapis wiadomości"
              : kind === "WORK_NOTE"
                ? "Zapisz notatkę wewnętrzną"
                : "Wyślij odpowiedź"}
          </button>
        </form>
      )}
      <div className="action-row">
        <h3>Wiadomości</h3>
        <button
          className="button button--secondary"
          disabled={busy}
          onClick={resetReads}
        >
          Odśwież komunikację
        </button>
      </div>
      {messages.loading ? (
        <PageState title="Ładowanie wiadomości…" />
      ) : messages.error ? (
        <PageState
          title="Błąd odczytu wiadomości"
          detail={messages.error}
          tone="error"
        />
      ) : !messages.data?.messages.length ? (
        <PageState title="Brak dostępnych wiadomości" />
      ) : (
        <ol className="incident-timeline" aria-label="Wiadomości incydentu">
          {messages.data.messages.map((m) => (
            <li key={m.id}>
              <h4>
                {m.type === "WORK_NOTE"
                  ? "Notatka wewnętrzna"
                  : "Odpowiedź dla zgłaszającego"}
              </h4>
              <p className="muted">
                {m.author_name} · {dateLabel(m.created_at)}
              </p>
              <p className="preserve-lines">{m.body}</p>
              <button
                className="button button--secondary"
                disabled={busy || !!fileRetry}
                onClick={() => {
                  setMessageId(m.id);
                  setVisibility(m.type === "WORK_NOTE" ? "INTERNAL" : "PUBLIC");
                }}
              >
                Powiąż kolejny plik z tą wiadomością
              </button>
            </li>
          ))}
        </ol>
      )}
      <div className="action-row">
        {before && (
          <button
            className="button button--secondary"
            onClick={() => page("messages_before_id")}
          >
            Najnowsze wiadomości
          </button>
        )}
        {messages.data?.next_before_id && (
          <button
            className="button button--secondary"
            onClick={() =>
              page("messages_before_id", messages.data!.next_before_id)
            }
          >
            Starsze wiadomości
          </button>
        )}
      </div>
      {(access.data?.reply || access.data?.write_notes) && (
        <form
          className="attachment-form"
          onSubmit={(event) => {
            event.preventDefault();
            void sendFile();
          }}
        >
          <h3>Dodaj załącznik</h3>
          <fieldset disabled={busy || !!fileRetry} className="form-fields">
            <label>
              Plik (do 10 MiB)
              <input
                ref={picker}
                type="file"
                onChange={(event) => setFile(event.target.files?.[0])}
              />
            </label>
            <label>
              Kategoria dostępu
              <select
                aria-label="Kategoria dostępu"
                value={visibility}
                onChange={(event) => setVisibility(event.target.value)}
                disabled={!!messageId}
              >
                {access.data.reply && (
                  <option value="PUBLIC">
                    Publiczny dla osób z dostępem do zgłoszenia
                  </option>
                )}
                {access.data.write_notes && (
                  <option value="INTERNAL">Wewnętrzny dla operatorów</option>
                )}
              </select>
            </label>
            {messageId && (
              <p>
                Plik odziedziczy dostęp wiadomości {messageId}.{" "}
                <button
                  type="button"
                  className="button button--secondary"
                  onClick={() => {
                    setMessageId("");
                    setVisibility(kind === "WORK_NOTE" ? "INTERNAL" : "PUBLIC");
                  }}
                >
                  Plik bez powiązanej wiadomości
                </button>
              </p>
            )}
          </fieldset>
          {percent !== undefined && (
            <div role="status">
              <progress
                max={100}
                value={percent}
                aria-label="Postęp przesyłania"
              />{" "}
              {percent}%
              {percent === 100 ? " · Oczekiwanie na potwierdzenie zapisu" : ""}
            </div>
          )}
          {fileRetry && (
            <p className="notice">
              Nie potwierdzono wyniku. Ponowienie zachowuje plik i klucz, aby
              uniknąć duplikatu.
            </p>
          )}
          <button className="button button--primary" disabled={busy || !file}>
            {fileRetry ? "Sprawdź zapis pliku" : "Prześlij plik"}
          </button>
        </form>
      )}
      <h3>Załączniki</h3>
      {files.loading ? (
        <PageState title="Ładowanie plików…" />
      ) : files.error ? (
        <PageState
          title="Błąd odczytu plików"
          detail={files.error}
          tone="error"
        />
      ) : !files.data?.files.length ? (
        <PageState title="Brak dostępnych załączników" />
      ) : (
        <ul className="attachment-list">
          {files.data.files.map((f) => (
            <li key={f.id}>
              <div>
                <strong>{f.file_name}</strong>
                <p className="muted">
                  {f.visibility === "INTERNAL" ? "Wewnętrzny" : "Publiczny"} ·{" "}
                  {f.content_type} · {f.size_bytes} B ·{" "}
                  {dateLabel(f.created_at)}
                  {f.message_id && ` · Wiadomość ${f.message_id}`}
                </p>
              </div>
              <div className="action-row">
                <button
                  className="button button--secondary"
                  disabled={busy}
                  onClick={() => void fileAction(f, false)}
                >
                  Pobierz {f.file_name}
                </button>
                {f.can_delete && (
                  <button
                    className="button button--secondary"
                    disabled={busy}
                    onClick={() => void fileAction(f, true)}
                  >
                    Usuń {f.file_name}
                  </button>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}
      <div className="action-row">
        {filesBefore && (
          <button
            className="button button--secondary"
            onClick={() => page("files_before_id")}
          >
            Najnowsze pliki
          </button>
        )}
        {files.data?.next_before_id && (
          <button
            className="button button--secondary"
            onClick={() => page("files_before_id", files.data!.next_before_id)}
          >
            Starsze pliki
          </button>
        )}
      </div>
    </>
  );
}
