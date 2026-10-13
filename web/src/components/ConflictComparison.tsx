import { auditChanges, displayValue, fieldLabel } from "./auditPresentation";
export function ConflictComparison({
  proposal,
  current,
}: {
  proposal: unknown;
  current: unknown;
}) {
  const rows = auditChanges(current, proposal);
  return rows.length ? (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th>Pole</th>
            <th>Twoja propozycja</th>
            <th>Aktualny zapis</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.field}>
              <th>{r.field.split(".").map(fieldLabel).join(" · ")}</th>
              <td className="preserve-lines">{displayValue(r.after)}</td>
              <td className="preserve-lines">{displayValue(r.before)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  ) : (
    <p>Pola są zgodne. Zmieniła się wersja rekordu.</p>
  );
}
