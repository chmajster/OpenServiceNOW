import { ConflictComparison } from "../components/ConflictComparison";
export function ConfigurationFeedback<T>({
  editor,
  proposal,
  current,
  resolve,
}: {
  editor: {
    busy: boolean;
    error?: string;
    notice?: string;
    uncertain: boolean;
    conflict?: T;
    retry: () => Promise<boolean>;
  };
  proposal: unknown;
  current?: unknown;
  resolve: (replace: boolean) => void;
}) {
  return (
    <>
      {editor.error && <p role="alert">{editor.error}</p>}
      {editor.notice && <p role="status">{editor.notice}</p>}
      {editor.uncertain && (
        <p role="status">
          Nie potwierdzono wyniku. Formularz zachowuje treść i klucz ostatniej
          operacji.{" "}
          <button
            type="button"
            disabled={editor.busy}
            onClick={() => void editor.retry()}
          >
            Potwierdź wynik konfiguracji
          </button>
        </p>
      )}
      {editor.conflict && (
        <section className="panel" aria-label="Konflikt konfiguracji">
          <h2>Konflikt wersji</h2>
          <ConflictComparison proposal={proposal} current={current} />
          <button type="button" onClick={() => resolve(false)}>
            Zachowaj propozycję i użyj aktualnej wersji
          </button>
          <button type="button" onClick={() => resolve(true)}>
            Użyj aktualnej konfiguracji
          </button>
        </section>
      )}
    </>
  );
}
