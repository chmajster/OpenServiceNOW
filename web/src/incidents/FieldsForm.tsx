import { useAuth } from "../auth/AuthContext";
import { majorMessages } from "./majorMessages";
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
import { MetadataFields } from "../metadata/MetadataFields";
import { useCMDBLocale } from "../cmdb/messages";

export function IncidentFields({
  draft,
  change,
  metadata,
  disabled,
  layout = "standard",
 historical = false,
}: {
  draft: FieldDraft;
  change: (draft: FieldDraft) => void;
  metadata: Metadata;
  disabled: boolean;
  layout?: "standard" | "record";
 historical?: boolean;
}) {
  const locale = useCMDBLocale();
 const t=majorMessages[locale];const {hasPermission}=useAuth();
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
      {!recordLayout&&!historical&&Boolean(metadata.templates?.length)&&<label>{t.template}<select value={draft.template_key??""} onChange={e=>{const v=metadata.templates?.find(x=>x.key===e.target.value);if(!v){change({...draft,template_key:"",template_version:"0"});return;}change({...draft,title:v.title,description:v.description,category:v.category,subcategory:v.subcategory,impact:String(v.impact),urgency:String(v.urgency),service_id:v.service_id,location:v.location,template_key:v.key,template_version:v.version,custom_fields:v.custom_fields??{}});}}><option value="">{t.noTemplate}</option>{metadata.templates?.map(v=><option key={v.key} value={v.key}>{v.labels[locale]} · {v.version}</option>)}</select></label>}
      {(hasPermission("service_portfolio.read")||hasPermission("service_portfolio.manage"))&&<RemoteCombobox kind="users" collection="items" cursor="before_id" label={t.service} endpoint="/api/v1/service-portfolio/items?lifecycle=ACTIVE" value={draft.service_id?{id:draft.service_id,label:draft.service_id}:null} onChange={v=>change({...draft,service_id:v?.id??""})} disabled={disabled}/>}
      <label className={labelClassName}>{t.location}<input maxLength={160} value={draft.location??""} onChange={e=>change({...draft,location:e.target.value})}/></label>
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
                subcategory: next?.subcategories.find(v=>!v.retired)?.value ?? "",
              });
            }}
          >
            {metadata.categories.filter(c=>!c.retired||historical&&c.value===draft.category).map((c) => (
              <option key={c.value} value={c.value}>
              {c.labels?.[locale] || c.label}
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
            {category?.subcategories.filter(o=>!o.retired||historical&&o.value===draft.subcategory).map((o) => (
              <option key={o.value} value={o.value}>
                {optionLabel([o], o.value)}
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
      <MetadataFields
        schema={metadata.custom_fields_schema}
        values={draft.custom_fields}
        baseValues={{
          title: draft.title,
          description: draft.description,
          affected_user_id: draft.affected?.id ?? "",
          category: draft.category,
          subcategory: draft.subcategory,
          impact: Number(draft.impact),
          urgency: Number(draft.urgency),
        }}
        onChange={(custom_fields) => change({ ...draft, custom_fields })}
        locale={locale}
        disabled={disabled}
      />
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
  const locale = useCMDBLocale();
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
      {metadata.custom_fields_schema.fields
        .filter((field) => field.active && !field.system && draft.custom_fields[field.key] !== undefined)
        .map((field) => (
          <div key={field.key}>
            <dt>{field.labels[locale] || field.labels.pl || field.key}</dt>
            <dd>{formatCustomValue(draft.custom_fields[field.key])}</dd>
          </div>
        ))}
    </dl>
  );
}

function formatCustomValue(value: unknown) {
  if (Array.isArray(value)) return value.join(", ");
  if (value === null || value === undefined || value === "") return "—";
  if (typeof value === "boolean") return value ? "Tak" : "Nie";
  if (typeof value === "object") return JSON.stringify(value);
  return String(value);
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
