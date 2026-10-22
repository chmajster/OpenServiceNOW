import type { ReactNode } from "react";
import { ConflictNotice } from "../components/ConflictNotice";
import { RemoteCombobox } from "../components/RemoteCombobox";
import {
  optionLabel,
  calculatedPriority,
  type FieldDraft,
  type Metadata,
  type Incident,
} from "./contracts";
import type { useIncidentForm } from "./hooks";

export function IncidentFields({
  draft,
  change,
  metadata,
  disabled,
  layout = "standard",
}: {
  draft: FieldDraft;
  change: (draft: FieldDraft) => void;
  metadata: Metadata;
  disabled: boolean;
  layout?: "standard" | "record";
}) {
  const category = metadata.categories.find((c) => c.value === draft.category);
  const recordLayout = layout === "record";
  const labelClassName = recordLayout
    ? "grid grid-cols-1 gap-x-3 gap-y-1 sm:grid-cols-[8rem_minmax(0,1fr)] sm:items-center"
    : undefined;
  const titleField = (
    <label className={labelClassName}>
      <span>{recordLayout ? "Krótki opis" : "Tytuł"}</span>
      <input
        required
        value={draft.title}
        onChange={(e) => change({ ...draft, title: e.target.value })}
      />
    </label>
  );
  const descriptionField = (
    <label className={labelClassName}>
      <span>Opis</span>
      <textarea
        aria-label="Opis"
        required
        rows={5}
        value={draft.description}
        onChange={(e) => change({ ...draft, description: e.target.value })}
      />
    </label>
  );
  return (
    <fieldset
      className={
        recordLayout
          ? "form-fields grid grid-cols-1 gap-y-2"
          : "form-fields"
      }
      disabled={disabled}
    >
      {!recordLayout ? titleField : null}
      {!recordLayout ? descriptionField : null}
      <RemoteCombobox
        kind="users"
        label="Osoba dotknięta problemem"
        endpoint="/api/v1/incidents/lookups/users?purpose=affected"
        className={
          recordLayout
            ? "incident-record-combobox grid grid-cols-[8rem_minmax(0,1fr)] items-center gap-x-3"
            : undefined
        }
        value={draft.affected}
        onChange={(affected) => change({ ...draft, affected })}
        disabled={disabled}
        clearable={false}
      />
      <div
        className={
          recordLayout
            ? "grid grid-cols-1 gap-y-2"
            : "two-column"
        }
      >
        <label className={labelClassName}>
          <span>Kategoria</span>
          <select
            aria-label="Kategoria"
            value={draft.category}
            onChange={(e) => {
              const next = metadata.categories.find(
                (c) => c.value === e.target.value,
              );
              change({
                ...draft,
                category: e.target.value,
                subcategory: next?.subcategories[0]?.value ?? "",
              });
            }}
          >
            {metadata.categories.map((c) => (
              <option key={c.value} value={c.value}>
                {c.label}
              </option>
            ))}
          </select>
        </label>
        <label className={labelClassName}>
          <span>Podkategoria</span>
          <select
            aria-label="Podkategoria"
            value={draft.subcategory}
            onChange={(e) => change({ ...draft, subcategory: e.target.value })}
          >
            {category?.subcategories.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </select>
        </label>
        <label className={labelClassName}>
          <span>Wpływ</span>
          <select
            aria-label="Wpływ"
            value={draft.impact}
            onChange={(e) => change({ ...draft, impact: e.target.value })}
          >
            <option value="1">Wysoki — wielu użytkowników</option>
            <option value="2">Średni — część użytkowników</option>
            <option value="3">Niski — pojedynczy użytkownik</option>
          </select>
        </label>
        <label className={labelClassName}>
          <span>Pilność</span>
          <select
            aria-label="Pilność"
            value={draft.urgency}
            onChange={(e) => change({ ...draft, urgency: e.target.value })}
          >
            <option value="1">Wysoka — obsługa natychmiast</option>
            <option value="2">Średnia — obsługa pilna</option>
            <option value="3">Niska — obsługa standardowa</option>
          </select>
        </label>
      </div>
      {!recordLayout ? (
        <p className="notice">
          Wyliczony priorytet:{" "}
          <strong>
            P{calculatedPriority(metadata, draft.impact, draft.urgency)}
          </strong>
          . Zmiana wpływu lub pilności przywraca priorytet z macierzy.
        </p>
      ) : null}
      {recordLayout ? titleField : null}
      {recordLayout ? descriptionField : null}
      <details>
        <summary>Macierz priorytetu</summary>
        <div className="table-scroll">
          <table>
            <caption>
              Wpływ × pilność (1 — wysoki poziom, 3 — niski poziom)
            </caption>
            <thead>
              <tr>
                <th scope="col">Wpływ</th>
                {[1, 2, 3].map((n) => (
                  <th scope="col" key={n}>
                    Pilność {n}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {metadata.priority_matrix.map((row, index) => (
                <tr key={index}>
                  <th scope="row">{index + 1}</th>
                  {row.map((p, column) => (
                    <td key={column}>P{p}</td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </details>
    </fieldset>
  );
}
export function FieldSummary({
  draft,
  metadata,
}: {
  draft: FieldDraft;
  metadata: Metadata;
}) {
  return (
    <dl className="detail-grid">
      <div>
        <dt>Tytuł</dt>
        <dd>{draft.title}</dd>
      </div>
      <div>
        <dt>Opis</dt>
        <dd className="preserve-lines">{draft.description}</dd>
      </div>
      <div>
        <dt>Osoba dotknięta problemem</dt>
        <dd>{draft.affected?.label ?? "Nie wybrano"}</dd>
      </div>
      <div>
        <dt>Kategoria</dt>
        <dd>
          {optionLabel(metadata.categories, draft.category)} /{" "}
          {optionLabel(
            metadata.categories.find((c) => c.value === draft.category)
              ?.subcategories ?? [],
            draft.subcategory,
          )}
        </dd>
      </div>
      <div>
        <dt>Wpływ / pilność</dt>
        <dd>
          {draft.impact} / {draft.urgency}
        </dd>
      </div>
    </dl>
  );
}
export function FormFeedback<T>({
  form,
  proposal,
  current,
  busy,
}: {
  form: ReturnType<typeof useIncidentForm<T>>;
  proposal: ReactNode;
  current: (record: Incident) => ReactNode;
  busy: boolean;
}) {
  return (
    <>
      {form.error && (
        <p className="notice notice--error" role="alert">
          {form.error}
        </p>
      )}
      {form.conflict !== undefined &&
        (form.conflict ? (
          <ConflictNotice
            current={current(form.conflict)}
            proposal={proposal}
            acceptCurrent={form.acceptCurrent}
            keepProposal={form.keepProposal}
          />
        ) : (
          <div className="notice" role="alert">
            <p>
              Twoja propozycja jest zachowana. Odczyt aktualnego stanu jest
              wymagany do rozstrzygnięcia konfliktu.
            </p>
            <button
              type="button"
              className="button button--secondary"
              disabled={busy}
              onClick={() => void form.retryConflict()}
            >
              Odczytaj aktualny stan
            </button>
          </div>
        ))}
      {form.uncertain && (
        <div className="notice">
          <p>
            Nie potwierdzono wyniku zapisu. Sprawdź aktualny stan przed
            ponowieniem.
          </p>
          <button
            type="button"
            className="button button--secondary"
            disabled={busy}
            onClick={() => void form.refreshUncertain()}
          >
            Sprawdź zapisany stan
          </button>
        </div>
      )}
    </>
  );
}
