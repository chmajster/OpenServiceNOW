import { useState } from "react";
import { flushSync } from "react-dom";
import { RemoteCombobox, type Choice } from "../components/RemoteCombobox";
import { UnsavedGuard } from "../incidents/UnsavedGuard";
import { useCreation } from "../requests/useCreation";
import type { Definition } from "./contracts";
import {
  buildApprovalWorkflow,
  initialApprovalWizardValues,
  validateApprovalWizardStep,
  type ApprovalWizardValues,
} from "./wizard";

const base = "/api/v1/workflow";
const steps = ["Proces", "Akceptacja", "Realizacja", "Podsumowanie"];

export function WorkflowWizard({
  back,
  created,
}: {
  back: () => void;
  created: (definition: Definition) => void;
}) {
  const [step, setStep] = useState(0);
  const [values, setValues] = useState<ApprovalWizardValues>(() => ({
    ...initialApprovalWizardValues,
  }));
  const [executorChoice, setExecutorChoice] = useState<Choice | null>(null);
  const [groupChoice, setGroupChoice] = useState<Choice | null>(null);
  const [approvalChoice, setApprovalChoice] = useState<Choice | null>(null);
  const [dirty, setDirty] = useState(false);
  const [validationError, setValidationError] = useState<string>();
  const creation = useCreation<Definition>();
  const locked = creation.busy || creation.uncertain;

  function edit<K extends keyof ApprovalWizardValues>(
    key: K,
    value: ApprovalWizardValues[K],
  ) {
    setValues((current) => ({ ...current, [key]: value }));
    setDirty(true);
    setValidationError(undefined);
  }

  async function submit() {
    const validation =
      step === steps.length - 1
        ? [0, 1, 2]
            .map((index) => validateApprovalWizardStep(index, values))
            .find(Boolean)
        : validateApprovalWizardStep(step, values);
    if (validation) {
      setValidationError(validation);
      return;
    }
    setValidationError(undefined);
    if (step < steps.length - 1) {
      setStep((current) => current + 1);
      return;
    }
    const definition = await creation.create(`${base}/definitions`, {
      version: "0",
      draft: buildApprovalWorkflow(values),
    });
    if (definition) {
      flushSync(() => setDirty(false));
      created(definition);
    }
  }

  function previous() {
    setValidationError(undefined);
    setStep((current) => Math.max(0, current - 1));
  }

  return (
    <section className="mx-auto my-6 w-full max-w-6xl rounded-2xl border border-sn-line bg-sn-paper shadow-sn-panel">
      <UnsavedGuard dirty={dirty} includeSearch />
      <header className="border-b border-sn-line bg-sn-canvas px-5 py-6 sm:px-8">
        <button
          type="button"
          disabled={locked}
          className="mb-5 inline-flex items-center gap-2 rounded-lg border border-sn-line bg-sn-paper px-3 py-2 text-sm font-semibold text-sn-ink hover:bg-slate-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-sn-teal disabled:opacity-50"
          onClick={back}
        >
          <span aria-hidden="true">←</span> Lista procesów
        </button>
        <p className="mb-1 text-xs font-bold uppercase tracking-[0.16em] text-sn-teal">
          Kreator procesu
        </p>
        <h2 className="text-2xl font-bold tracking-tight text-sn-ink sm:text-3xl">
          Zbuduj proces z akceptacją
        </h2>
        <p className="mt-2 max-w-3xl text-sm leading-6 text-sn-muted">
          Ustaw osoby odpowiedzialne i terminy. Kreator przygotuje kompletny
          przepływ, który po utworzeniu szkicu możesz jeszcze edytować.
        </p>
        <ol
          className="mt-6 grid grid-cols-2 gap-2 sm:grid-cols-4"
          aria-label="Etapy tworzenia procesu"
        >
          {steps.map((label, index) => {
            const active = index === step;
            const complete = index < step;
            return (
              <li key={label} aria-current={active ? "step" : undefined}>
                <div
                  className={`flex min-h-12 items-center gap-2 rounded-xl border px-3 py-2 text-sm font-semibold ${
                    active
                      ? "border-sn-teal bg-sn-teal text-white"
                      : complete
                        ? "border-emerald-200 bg-emerald-50 text-emerald-900"
                        : "border-sn-line bg-sn-paper text-sn-muted"
                  }`}
                >
                  <span className="grid size-6 shrink-0 place-items-center rounded-full border border-current text-xs">
                    {complete ? "✓" : index + 1}
                  </span>
                  {label}
                </div>
              </li>
            );
          })}
        </ol>
      </header>

      <form
        className="px-5 py-6 sm:px-8 sm:py-8"
        onSubmit={(event) => {
          event.preventDefault();
          void submit();
        }}
      >
        <div className="min-h-72">
          {step === 0 ? (
            <section aria-labelledby="wizard-step-title">
              <StepHeading
                number="01"
                title="Dane procesu i realizacja"
                description="Wskaż, kto uruchamia zadania oraz w jakiej grupie będą one obsługiwane."
              />
              <div className="mt-6 grid gap-5 md:grid-cols-2">
                <label className="grid content-start gap-2 text-sm font-semibold text-sn-ink md:col-span-2">
                  Nazwa procesu
                  <input
                    autoFocus
                    required
                    maxLength={400}
                    value={values.name}
                    disabled={locked}
                    onChange={(event) => edit("name", event.target.value)}
                    className="min-h-11 rounded-lg border border-sn-line bg-white px-3 font-normal outline-none transition focus:border-sn-teal focus:ring-2 focus:ring-sn-teal/20 disabled:bg-slate-100"
                    placeholder="np. Akceptacja zakupu sprzętu"
                  />
                  <small className="font-normal text-sn-muted">
                    Ta nazwa będzie widoczna przy uruchomieniu i w historii procesu.
                  </small>
                </label>
                <IdentityField
                  kind="users"
                  label="Tożsamość wykonawcza"
                  value={executorChoice}
                  disabled={locked}
                  onChange={(choice) => {
                    setExecutorChoice(choice);
                    edit("executor_id", choice?.id ?? "0");
                  }}
                />
                <IdentityField
                  kind="groups"
                  label="Grupa realizacji procesu"
                  value={groupChoice}
                  disabled={locked}
                  onChange={(choice) => {
                    setGroupChoice(choice);
                    edit("group_id", choice?.id ?? "0");
                  }}
                />
                <label className="grid content-start gap-2 text-sm font-semibold text-sn-ink md:col-span-2">
                  Akceptacja własnego wniosku
                  <select
                    value={values.self_approval}
                    disabled={locked}
                    onChange={(event) =>
                      edit("self_approval", event.target.value as "DENY" | "ALLOW")
                    }
                    className="min-h-11 rounded-lg border border-sn-line bg-white px-3 font-normal outline-none transition focus:border-sn-teal focus:ring-2 focus:ring-sn-teal/20 disabled:bg-slate-100"
                  >
                    <option value="DENY">
                      Zgłaszający nie może zaakceptować własnego wniosku
                    </option>
                    <option value="ALLOW">
                      Może, jeśli ma uprawnienie do akceptacji
                    </option>
                  </select>
                  <small className="font-normal text-sn-muted">
                    Uprawnienia i członkostwo są ponownie sprawdzane przez serwer
                    przy publikacji i wykonaniu.
                  </small>
                </label>
              </div>
            </section>
          ) : null}

          {step === 1 ? (
            <section aria-labelledby="wizard-step-title">
              <StepHeading
                number="02"
                title="Kto podejmuje decyzję?"
                description="Akceptacja jest zapisywana w procesie. Odrzucenie lub brak decyzji w terminie anuluje wniosek."
              />
              <div className="mt-6 grid gap-5 md:grid-cols-2">
                <label className="grid content-start gap-2 text-sm font-semibold text-sn-ink">
                  Akceptuje
                  <select
                    value={values.approval_target_type}
                    disabled={locked}
                    onChange={(event) => {
                      const type = event.target.value as "USER" | "GROUP";
                      edit("approval_target_type", type);
                      edit("approval_target_id", "0");
                      setApprovalChoice(null);
                    }}
                    className="min-h-11 rounded-lg border border-sn-line bg-white px-3 font-normal outline-none transition focus:border-sn-teal focus:ring-2 focus:ring-sn-teal/20 disabled:bg-slate-100"
                  >
                    <option value="GROUP">Grupa osób</option>
                    <option value="USER">Jedna osoba</option>
                  </select>
                </label>
                <IdentityField
                  kind={values.approval_target_type === "GROUP" ? "groups" : "users"}
                  label={values.approval_target_type === "GROUP" ? "Grupa akceptująca" : "Osoba akceptująca"}
                  value={approvalChoice}
                  disabled={locked}
                  onChange={(choice) => {
                    setApprovalChoice(choice);
                    edit("approval_target_id", choice?.id ?? "0");
                  }}
                />
                {values.approval_target_type === "GROUP" ? (
                  <label className="grid content-start gap-2 text-sm font-semibold text-sn-ink">
                    Polityka akceptacji
                    <select
                      value={values.approval_policy}
                      disabled={locked}
                      onChange={(event) =>
                        edit("approval_policy", event.target.value as "ANY" | "ALL")
                      }
                      className="min-h-11 rounded-lg border border-sn-line bg-white px-3 font-normal outline-none transition focus:border-sn-teal focus:ring-2 focus:ring-sn-teal/20 disabled:bg-slate-100"
                    >
                      <option value="ANY">Wystarczy jedna osoba</option>
                      <option value="ALL">Wymagana decyzja każdej osoby</option>
                    </select>
                  </label>
                ) : null}
                <label className="grid content-start gap-2 text-sm font-semibold text-sn-ink">
                  Termin decyzji (godziny)
                  <input
                    required
                    type="number"
                    min={1}
                    max={2160}
                    step={1}
                    value={values.approval_hours}
                    disabled={locked}
                    onChange={(event) => edit("approval_hours", event.target.value)}
                    className="min-h-11 rounded-lg border border-sn-line bg-white px-3 font-normal outline-none transition focus:border-sn-teal focus:ring-2 focus:ring-sn-teal/20 disabled:bg-slate-100"
                  />
                  <small className="font-normal text-sn-muted">Od 1 godziny do 90 dni.</small>
                </label>
              </div>
              <div className="mt-6 rounded-xl border border-amber-200 bg-amber-50 p-4 text-sm leading-6 text-amber-950">
                Osoby z grupy akceptującej muszą mieć uprawnienie do podejmowania
                decyzji. Serwer sprawdzi grupę, aktywność kont i uprawnienia przed
                publikacją.
              </div>
            </section>
          ) : null}

          {step === 2 ? (
            <section aria-labelledby="wizard-step-title">
              <StepHeading
                number="03"
                title="Zadanie po akceptacji"
                description="Po pozytywnej decyzji system utworzy zadanie dla wybranej grupy realizacji."
              />
              <div className="mt-6 grid gap-5">
                <label className="grid content-start gap-2 text-sm font-semibold text-sn-ink">
                  Tytuł zadania
                  <input
                    required
                    maxLength={400}
                    value={values.fulfillment_title}
                    disabled={locked}
                    onChange={(event) => edit("fulfillment_title", event.target.value)}
                    className="min-h-11 rounded-lg border border-sn-line bg-white px-3 font-normal outline-none transition focus:border-sn-teal focus:ring-2 focus:ring-sn-teal/20 disabled:bg-slate-100"
                    placeholder="np. Przygotuj i skonfiguruj sprzęt"
                  />
                </label>
                <label className="grid content-start gap-2 text-sm font-semibold text-sn-ink">
                  Instrukcje dla realizującego
                  <textarea
                    rows={5}
                    maxLength={20000}
                    value={values.fulfillment_instructions}
                    disabled={locked}
                    onChange={(event) => edit("fulfillment_instructions", event.target.value)}
                    className="min-h-28 rounded-lg border border-sn-line bg-white px-3 py-2 font-normal outline-none transition focus:border-sn-teal focus:ring-2 focus:ring-sn-teal/20 disabled:bg-slate-100"
                    placeholder="Opisz wymagane działania i sposób potwierdzenia realizacji."
                  />
                  <small className="font-normal text-sn-muted">
                    {Array.from(values.fulfillment_instructions).length}/10 000 znaków
                  </small>
                </label>
                <label className="grid max-w-sm content-start gap-2 text-sm font-semibold text-sn-ink">
                  Termin wykonania (dni)
                  <input
                    required
                    type="number"
                    min={1}
                    max={365}
                    step={1}
                    value={values.fulfillment_days}
                    disabled={locked}
                    onChange={(event) => edit("fulfillment_days", event.target.value)}
                    className="min-h-11 rounded-lg border border-sn-line bg-white px-3 font-normal outline-none transition focus:border-sn-teal focus:ring-2 focus:ring-sn-teal/20 disabled:bg-slate-100"
                  />
                  <small className="font-normal text-sn-muted">
                    Po przekroczeniu terminu wniosek zostanie anulowany.
                  </small>
                </label>
              </div>
            </section>
          ) : null}

          {step === 3 ? (
            <section aria-labelledby="wizard-step-title">
              <StepHeading
                number="04"
                title="Sprawdź przepływ"
                description="Potwierdź kolejność kroków i ścieżki końcowe. Szczegóły możesz zmienić w edytorze po utworzeniu szkicu."
              />
              <div className="mt-6 rounded-xl border border-sn-line bg-sn-canvas p-4 sm:p-6">
                <div className="grid gap-3 md:grid-cols-4">
                  <FlowCard number="1" title="Nowy wniosek" detail={values.name.trim() || "Nazwa procesu"} />
                  <FlowCard
                    number="2"
                    title="Akceptacja"
                    detail={`${approvalChoice?.label ?? "Nie wybrano akceptującego"} · ${values.approval_target_type === "GROUP" ? values.approval_policy === "ANY" ? "wystarczy jedna osoba" : "decyzja każdej osoby" : "jedna osoba"} · ${values.approval_hours || "0"} godz.`}
                  />
                  <FlowCard
                    number="3"
                    title="Realizacja"
                    detail={`${values.fulfillment_title.trim() || "Tytuł zadania"} · ${groupChoice?.label ?? "nie wybrano grupy"} · do ${values.fulfillment_days || "0"} dni`}
                  />
                  <FlowCard
                    number="4"
                    title="Zamknięcie"
                    detail="Ukończenie zadania zmienia wniosek na zrealizowany; zgłaszający dostaje powiadomienie."
                  />
                </div>
                <div className="mt-4 grid gap-3 md:grid-cols-2">
                  <div className="rounded-lg border border-emerald-200 bg-emerald-50 p-4 text-sm leading-6 text-emerald-950">
                    <strong className="block">Po akceptacji</strong>
                    Tworzy się zadanie realizacji. Po wykonaniu wniosek otrzymuje
                    stan „Zrealizowany”.
                  </div>
                  <div className="rounded-lg border border-rose-200 bg-rose-50 p-4 text-sm leading-6 text-rose-950">
                    <strong className="block">Odrzucenie lub przekroczenie terminu</strong>
                    Wniosek otrzymuje stan „Anulowany”, a zgłaszający jest
                    powiadamiany.
                  </div>
                </div>
              </div>
              <dl className="mt-5 grid gap-3 rounded-xl border border-sn-line p-4 text-sm sm:grid-cols-2">
                <ReviewValue label="Wykonawca" value={executorChoice?.label ?? "Nie wybrano"} />
                <ReviewValue label="Grupa realizacji" value={groupChoice?.label ?? "Nie wybrano"} />
                <ReviewValue
                  label="Własna akceptacja"
                  value={values.self_approval === "DENY" ? "Zabroniona" : "Dozwolona z uprawnieniem"}
                />
                <ReviewValue
                  label="Akceptacja"
                  value={values.approval_target_type === "GROUP" ? values.approval_policy === "ANY" ? "Jedna osoba z grupy" : "Każda osoba z grupy" : "Wybrana osoba"}
                />
              </dl>
              <p className="mt-4 text-sm leading-6 text-sn-muted">
                Utworzenie zapisze wersję roboczą. Publikacja wymaga osobnej
                weryfikacji uprawnień i członkostwa przez serwer.
              </p>
            </section>
          ) : null}
        </div>

        {validationError ? (
          <p role="alert" className="mt-5 rounded-lg border border-rose-200 bg-rose-50 px-4 py-3 text-sm font-medium text-rose-900">
            {validationError}
          </p>
        ) : null}
        {creation.error ? (
          <p role="alert" className="mt-5 rounded-lg border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-900">
            {creation.error}
          </p>
        ) : null}
        {creation.uncertain ? (
          <p role="status" className="mt-3 text-sm text-sn-muted">
            Nie udało się potwierdzić wyniku zapisu. Ponowienie użyje tego samego
            klucza, aby nie utworzyć drugiego procesu.
          </p>
        ) : null}

        <div className="mt-8 flex flex-col-reverse justify-between gap-3 border-t border-sn-line pt-5 sm:flex-row sm:items-center">
          <div>
            {step > 0 ? (
              <button
                type="button"
                disabled={locked}
                className="min-h-11 rounded-lg px-4 py-2 text-sm font-semibold text-sn-muted hover:bg-slate-100 hover:text-sn-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-sn-teal disabled:opacity-50"
                onClick={previous}
              >
                Wstecz
              </button>
            ) : null}
          </div>
          <div className="flex flex-col-reverse gap-3 sm:flex-row">
            {step < steps.length - 1 ? (
              <button
                type="submit"
                disabled={locked}
                className="min-h-11 rounded-lg bg-sn-teal px-5 py-2 text-sm font-bold text-white shadow-sm transition hover:bg-sn-teal-dark focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-sn-teal disabled:cursor-wait disabled:opacity-60"
              >
                Dalej <span aria-hidden="true">→</span>
              </button>
            ) : (
              <button
                type="submit"
                disabled={creation.busy}
                className="min-h-11 rounded-lg bg-sn-teal px-5 py-2 text-sm font-bold text-white shadow-sm transition hover:bg-sn-teal-dark focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-sn-teal disabled:cursor-wait disabled:opacity-60"
              >
                {creation.busy
                  ? "Zapisywanie szkicu…"
                  : creation.uncertain
                    ? "Potwierdź wynik zapisu"
                    : "Zapisz szkic procesu"}
              </button>
            )}
          </div>
        </div>
      </form>
    </section>
  );
}

function StepHeading({
  number,
  title,
  description,
}: {
  number: string;
  title: string;
  description: string;
}) {
  return (
    <div>
      <p className="text-xs font-bold tracking-[0.14em] text-sn-teal">KROK {number}</p>
      <h3 id="wizard-step-title" className="mt-1 text-xl font-bold text-sn-ink">
        {title}
      </h3>
      <p className="mt-2 max-w-3xl text-sm leading-6 text-sn-muted">{description}</p>
    </div>
  );
}

function IdentityField({
  kind,
  label,
  value,
  disabled,
  onChange,
}: {
  kind: "users" | "groups";
  label: string;
  value: Choice | null;
  disabled: boolean;
  onChange: (choice: Choice | null) => void;
}) {
  return (
    <div className="grid content-start gap-2 text-sm font-semibold text-sn-ink">
      <RemoteCombobox
        kind={kind}
        label={label}
        endpoint={`${base}/lookups/${kind}`}
        value={value}
        disabled={disabled}
        onChange={onChange}
      />
    </div>
  );
}

function FlowCard({
  number,
  title,
  detail,
}: {
  number: string;
  title: string;
  detail: string;
}) {
  return (
    <article className="min-w-0 rounded-xl border border-sn-line bg-white p-4 shadow-sm">
      <span className="grid size-7 place-items-center rounded-full bg-sn-teal text-xs font-bold text-white">
        {number}
      </span>
      <h4 className="mt-3 font-bold text-sn-ink">{title}</h4>
      <p className="mt-1 break-words text-sm leading-5 text-sn-muted">{detail}</p>
    </article>
  );
}

function ReviewValue({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs font-medium text-sn-muted">{label}</dt>
      <dd className="mt-1 break-words font-semibold text-sn-ink">{value}</dd>
    </div>
  );
}
