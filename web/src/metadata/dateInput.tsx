const messages = {
  pl: {
    invalid: "Nieprawidłowa data w wybranej strefie czasowej.",
    ambiguous: "Wybierz przesunięcie czasu dla powtórzonej godziny.",
    offset: "Przesunięcie czasu",
  },
  en: {
    invalid: "Invalid local time in the selected time zone.",
    ambiguous: "Choose an offset for the repeated local time.",
    offset: "UTC offset",
  },
};
const zone = () =>
  typeof document === "undefined"
    ? "UTC"
    : document.documentElement.dataset.timeZone || "UTC";
const locale = () =>
  typeof document !== "undefined" && document.documentElement.lang === "en"
    ? "en"
    : "pl";
let lastZone = "",
  formatter: Intl.DateTimeFormat | undefined;
function format(value: Date) {
  const tz = zone();
  if (tz !== lastZone || !formatter) {
    lastZone = tz;
    formatter = new Intl.DateTimeFormat("en-CA-u-ca-iso8601", {
      timeZone: tz,
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
      hourCycle: "h23",
    });
  }
  const parts = formatter.formatToParts(value);
  const part = (type: string) =>
    parts.find((p) => p.type === type)?.value ?? "";
  return `${part("year")}-${part("month")}-${part("day")}T${part("hour")}:${part("minute")}:${part("second")}`;
}
function normalized(value: string) {
  const text = value.split("#")[0] ?? "";
  return text.length === 16 ? text + ":00" : text;
}
export function dateInputCandidates(value: string) {
  const text = normalized(value);
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}$/.test(text)) return [];
  const base = Date.parse(text + "Z");
  if (!Number.isFinite(base)) return [];
  if (zone() === "UTC") return format(new Date(base)) === text ? [base] : [];
  const out: number[] = [];
  for (let offset = -14 * 60; offset <= 14 * 60; offset += 15) {
    const candidate = base + offset * 60000;
    if (format(new Date(candidate)) === text) out.push(candidate);
  }
  return out;
}
export function dateInputValue(value?: string) {
  if (!value) return "";
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return "";
  const text = format(date),
    candidates = dateInputCandidates(text);
  return candidates.length > 1
    ? `${text}#${Math.floor(date.getTime() / 1000) * 1000}`
    : text;
}
export function parseDateInput(value: string) {
  if (!value) return null;
  const candidates = dateInputCandidates(value);
  if (!candidates.length) throw new Error(messages[locale()].invalid);
  const selected = value.split("#")[1];
  if (candidates.length > 1 && !selected)
    throw new Error(messages[locale()].ambiguous);
  const result = selected ? Number(selected) : candidates[0]!;
  if (!candidates.includes(result)) throw new Error(messages[locale()].invalid);
  return new Date(result).toISOString();
}
export function OrganizationDateInput({
  label,
  value,
  onChange,
  required = false,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  required?: boolean;
}) {
  const candidates = dateInputCandidates(value),
    selected = value.split("#")[1] ?? "";
  return (
    <div>
      <label>
        {label} <small>({zone()})</small>
        <input
          type="datetime-local"
          step={1}
          required={required}
          value={value.split("#")[0] ?? ""}
          onChange={(e) => onChange(e.target.value)}
        />
      </label>
      {candidates.length > 1 && (
        <label>
          {messages[locale()].offset}
          <select
            required
            value={selected}
            onChange={(e) => onChange(`${normalized(value)}#${e.target.value}`)}
          >
            <option value="">—</option>
            {candidates.map((candidate) => (
              <option key={candidate} value={candidate}>
                {new Intl.DateTimeFormat("en", {
                  timeZone: zone(),
                  timeZoneName: "shortOffset",
                  hour: "2-digit",
                  minute: "2-digit",
                }).format(new Date(candidate))}
              </option>
            ))}
          </select>
        </label>
      )}
    </div>
  );
}
