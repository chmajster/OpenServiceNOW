import type { ReactNode } from "react";
export function ConflictNotice({
  current,
  proposal,
  acceptCurrent,
  keepProposal,
}: {
  current: ReactNode;
  proposal: ReactNode;
  acceptCurrent: () => void;
  keepProposal: () => void;
}) {
  return (
    <section className="conflict-notice" aria-label="Konflikt wersji">
      <h2>Konflikt wersji</h2>
      <p role="alert">
        Dane zmieniły się od rozpoczęcia edycji. Twoja propozycja została
        zachowana. Wybierz, jak kontynuować.
      </p>
      <div className="two-column">
        <div>
          <h3>Aktualny stan</h3>
          {current}
        </div>
        <div>
          <h3>Twoja propozycja</h3>
          {proposal}
        </div>
      </div>
      <div className="action-row">
        <button
          type="button"
          className="button button--secondary"
          onClick={acceptCurrent}
        >
          Przyjmij aktualny stan
        </button>
        <button
          type="button"
          className="button button--primary"
          onClick={keepProposal}
        >
          Zachowaj propozycję do ponownego zapisu
        </button>
      </div>
    </section>
  );
}
