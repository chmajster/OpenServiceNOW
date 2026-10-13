import { useState } from "react";
import { Link } from "react-router-dom";
import { InventoryChoice, PageFeedback } from "../cmdb/components";
import { type CI, labels } from "../cmdb/contracts";
import { type Choice } from "../components/RemoteCombobox";
import { useIncidentQuery } from "../incidents/hooks";
export function CIReference({ id }: { id: string }) {
  const p = useIncidentQuery<CI>(`/api/v1/cmdb/cis/${id}`);
  return (
    <>
      <PageFeedback loading={p.loading} error={p.error} empty={false} />
      {p.data && (
        <Link to={`/cmdb/${id}`}>
          {p.data.identifier} · {p.data.name} · {labels[p.data.class]}
          {p.data.archived ? " · Archiwum" : ""}
        </Link>
      )}
    </>
  );
}
export function CISet({
  ids,
  change,
  disabled,
}: {
  ids: string[];
  change?: (ids: string[]) => void;
  disabled?: boolean;
}) {
  const [choice, setChoice] = useState<Choice | null>(null),
    selected = useIncidentQuery<CI>(
      choice ? `/api/v1/cmdb/cis/${choice.id}` : null,
    );
  return (
    <section>
      <h3>Zestaw CI szablonu</h3>
      <ul>
        {ids.map((id) => (
          <li key={id}>
            <CIReference id={id} />
            {change && (
              <button
                type="button"
                disabled={disabled}
                onClick={() => change(ids.filter((v) => v !== id))}
              >
                Usuń CI z szablonu
              </button>
            )}
          </li>
        ))}
      </ul>
      {!ids.length && <p>Wybierz co najmniej jeden CI.</p>}
      {change && (
        <>
          <InventoryChoice
            label="CI szablonu"
            value={choice}
            onChange={setChoice}
            endpoint="/api/v1/cmdb/cis?archived=false"
            disabled={disabled}
          />
          <PageFeedback
            loading={!!choice && selected.loading}
            error={selected.error}
            empty={false}
          />
          {selected.data && (
            <p>
              Podgląd:{" "}
              <Link to={`/cmdb/${selected.data.id}`}>
                {selected.data.identifier} · {selected.data.name}
              </Link>{" "}
              · {labels[selected.data.class]}
            </p>
          )}
          <button
            type="button"
            disabled={
              disabled ||
              !selected.data ||
              selected.data.archived ||
              ids.length >= 20 ||
              ids.includes(selected.data.id)
            }
            onClick={() => {
              change([...ids, selected.data!.id]);
              setChoice(null);
            }}
          >
            Dodaj CI do szablonu
          </button>
        </>
      )}
    </section>
  );
}
