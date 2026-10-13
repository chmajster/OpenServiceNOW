import { useEffect, useRef, useState } from "react";
import { useRemotePage } from "../components/useRemotePage";
import { api } from "../api/client";
import { LoadState } from "../requests/LoadState";
import type { Config, Definition } from "./contracts";
export function WorkflowPicker({
  id,
  version,
  change,
}: {
  id?: string;
  version?: string;
  change: (id: string, version: string, group: string) => void;
}) {
  const [q, setQ] = useState(""),
    [busy, setBusy] = useState(false),
    [error, setError] = useState<string>();
  const p = useRemotePage<Definition>(
    `/api/v1/workflow/definitions?limit=25&q=${encodeURIComponent(q)}`,
    "items",
  );
  const selected = id && version ? `${id}:${version}` : "";
  const request = useRef<AbortController | null>(null);
  useEffect(() => () => request.current?.abort(), []);
  return (
    <section>
      <label>
        Wyszukaj opublikowany proces
        <input
          maxLength={200}
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
      </label>
      <label>
        Wersja procesu
        <select
          aria-label="Wersja procesu"
          required
          disabled={busy || p.loading}
          value={selected}
          onChange={async (e) => {
            const [next, v] = e.target.value.split(":");
            if (!next || !v) return;
            request.current?.abort();
            const controller = new AbortController();
            request.current = controller;
            setBusy(true);
            setError(undefined);
            try {
              const c = await api<Config>(
                `/api/v1/workflow/versions/${next}/${v}`,
                { signal: controller.signal },
              );
              if (controller.signal.aborted) return;
              change(next, v, c.group_id);
            } catch (e) {
              if (controller.signal.aborted) return;
              setError(e instanceof Error ? e.message : "Nie pobrano procesu");
            } finally {
              if (!controller.signal.aborted) setBusy(false);
            }
          }}
        >
          <option value="">Wybierz…</option>
          {selected &&
          !p.items.some(
            (d) => `${d.id}:${d.published_version}` === selected,
          ) ? (
            <option value={selected}>
              Proces #{id} · wersja {version} (zachowana)
            </option>
          ) : null}
          {p.items
            .filter((d) => d.published_version)
            .map((d) => (
              <option key={d.id} value={`${d.id}:${d.published_version}`}>
                {d.draft.name} · wersja {d.published_version}
              </option>
            ))}
        </select>
      </label>
      <LoadState
        loading={p.loading || busy}
        error={error ?? p.error}
        retry={p.retry}
      />
      {p.next ? (
        <button
          type="button"
          className="button button--secondary"
          onClick={() => void p.more()}
        >
          Więcej procesów
        </button>
      ) : null}
      <p>
        Wybrana wersja jest przypięta do publikacji usługi. Nowa publikacja
        procesu nie zmienia tej wersji. Zadania powstaną wyłącznie w krokach po
        akceptacji.
      </p>
    </section>
  );
}
