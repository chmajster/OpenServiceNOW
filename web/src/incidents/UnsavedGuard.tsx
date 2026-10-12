import { useEffect, useRef } from "react";
import { useBlocker } from "react-router-dom";

export function UnsavedGuard({ dirty }: { dirty: boolean }) {
  const blocker = useBlocker(
    ({ currentLocation, nextLocation }) =>
      dirty && currentLocation.pathname !== nextLocation.pathname,
  );
  const stay = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    if (!dirty) return;
    const unload = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", unload);
    return () => window.removeEventListener("beforeunload", unload);
  }, [dirty]);
  useEffect(() => {
    if (blocker.state === "blocked") stay.current?.focus();
  }, [blocker.state]);
  if (blocker.state !== "blocked") return null;
  return (
    <div className="dialog-backdrop">
      <section
        className="panel confirm-dialog"
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="leave-title"
        onKeyDown={(event) => {
          if (event.key === "Escape") blocker.reset();
          if (event.key === "Tab") {
            const buttons = event.currentTarget.querySelectorAll("button");
            const first = buttons[0],
              last = buttons[buttons.length - 1];
            if (event.shiftKey && document.activeElement === first) {
              event.preventDefault();
              last?.focus();
            } else if (!event.shiftKey && document.activeElement === last) {
              event.preventDefault();
              first?.focus();
            }
          }
        }}
      >
        <h2 id="leave-title">Niezapisana praca</h2>
        <p>
          Opuszczenie strony usunie niezapisane dane. Zapisz je lub świadomie
          zrezygnuj z edycji.
        </p>
        <div className="action-row">
          <button
            ref={stay}
            className="button button--primary"
            onClick={() => blocker.reset()}
          >
            Pozostań na stronie
          </button>
          <button
            className="button button--secondary"
            onClick={() => blocker.proceed()}
          >
            Opuść bez zapisu
          </button>
        </div>
      </section>
    </div>
  );
}
