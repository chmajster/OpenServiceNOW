export type KnowledgeTemplate = {
  id: string;
  name: string;
  description: string;
  title: string;
  body: string;
};

export const knowledgeTemplates: KnowledgeTemplate[] = [
  {
    id: "runbook",
    name: "Instrukcja operacyjna",
    description: "Powtarzalna procedura z warunkami, krokami i wycofaniem zmian.",
    title: "Instrukcja: ",
    body: `# Cel

Opisz oczekiwany rezultat procedury.

## Warunki wstępne

- Wymagane uprawnienia
- Potrzebne narzędzia i dane

## Procedura

1. Pierwszy krok
2. Drugi krok
3. Potwierdzenie wyniku

## Wycofanie zmian

Opisz bezpieczny sposób powrotu do poprzedniego stanu.

## Weryfikacja

- [ ] Wynik sprawdzony
- [ ] Logi i monitoring sprawdzone
`,
  },
  {
    id: "incident",
    name: "Analiza incydentu",
    description: "Chronologia, wpływ, przyczyna źródłowa i działania naprawcze.",
    title: "Analiza incydentu: ",
    body: `# Podsumowanie

Krótko opisz zdarzenie i jego wpływ.

## Chronologia

- Czas wykrycia — zdarzenie
- Czas rozwiązania — zdarzenie

## Przyczyna źródłowa

Opisz potwierdzoną przyczynę.

## Działania naprawcze

- [ ] Działanie, właściciel, termin

## Wnioski

Co zmienimy, aby ograniczyć ryzyko powtórzenia?
`,
  },
  {
    id: "decision",
    name: "Decyzja architektoniczna",
    description: "Kontekst, rozważane warianty i konsekwencje decyzji.",
    title: "ADR: ",
    body: `# Status

Proponowana

## Kontekst

Jaki problem rozwiązujemy i jakie mamy ograniczenia?

## Rozważane warianty

1. Wariant A
2. Wariant B

## Decyzja

Opisz wybrany wariant i uzasadnienie.

## Konsekwencje

Wymień korzyści, koszty i ryzyka.
`,
  },
  {
    id: "onboarding",
    name: "Onboarding zespołu",
    description: "Pierwsze kroki, dostępy, środowisko i kontakty.",
    title: "Onboarding: ",
    body: `# Witamy w zespole

Najważniejsze informacje na początek.

## Pierwszy dzień

- [ ] Dostępy i konta
- [ ] Konfiguracja środowiska
- [ ] Spotkania wprowadzające

## Pierwszy tydzień

- [ ] Poznanie usług i odpowiedzialności
- [ ] Pierwsze zadanie

## Kontakty i eskalacja

Opisz właścicieli systemów i kanały wsparcia.
`,
  },
];
