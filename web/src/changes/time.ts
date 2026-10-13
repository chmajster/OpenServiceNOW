function parts(at: number, zone: string) {
  const values = new Intl.DateTimeFormat("en-CA", {
    timeZone: zone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hourCycle: "h23",
  }).formatToParts(new Date(at));
  const get = (key: string) => values.find((p) => p.type === key)!.value;
  return `${get("year")}-${get("month")}-${get("day")}T${get("hour")}:${get("minute")}`;
}
export function localTime(instant: string, zone: string) {
  return parts(Date.parse(instant), zone);
}
// A local wall-clock value can denote zero, one or two real instants around
// DST. Preserve the choice explicitly instead of silently normalizing it.
export function localCandidates(local: string, zone: string): string[] {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(local)) return [];
  const naive = Date.parse(`${local}:00Z`);
  if (!Number.isFinite(naive)) return [];
  const candidates = new Set<string>();
  for (let h = -36; h <= 36; h += 6) {
    const sample = naive + h * 3600000;
    const displayed = parts(sample, zone);
    const offset = Date.parse(`${displayed}:00Z`) - sample;
    const candidate = naive - offset;
    if (parts(candidate, zone) === local)
      candidates.add(new Date(candidate).toISOString());
  }
  return [...candidates].sort();
}
