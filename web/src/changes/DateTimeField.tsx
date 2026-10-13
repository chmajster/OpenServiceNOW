import { useEffect, useState, useRef } from "react";
import { localCandidates, localTime } from "./time";

export function DateTimeField({
  label,
  value,
  zone,
  onChange,
  required = false,
}: {
  label: string;
  value: string | null;
  zone: string;
  onChange: (v: string | null) => void;
  required?: boolean;
}) {
  const display = (instant: string) => {
    try {
      return localTime(instant, zone);
    } catch {
      return "";
    }
  };
  const localEdit = useRef(false),
    input = useRef<HTMLInputElement>(null);
  const [wall, setWall] = useState(value ? display(value) : "");
  useEffect(() => {
    if (value) {
      setWall(display(value));
      localEdit.current = false;
    } else if (localEdit.current) {
      localEdit.current = false;
    } else {
      setWall("");
    }
  }, [value, zone]);
  let candidates: string[] = [];
  let error = "";
  try {
    candidates = wall ? localCandidates(wall, zone) : [];
  } catch {
    error = "Nieprawidłowa strefa IANA.";
  }
  const selected = value
    ? (candidates.find((v) => Date.parse(v) === Date.parse(value)) ?? "")
    : "";
  useEffect(() => {
    input.current?.setCustomValidity(
      wall && !candidates.length
        ? error || "Ten czas lokalny nie istnieje w wybranej strefie."
        : "",
    );
  }, [wall, error, candidates.length]);
  return (
    <div>
      <label>
        {label}
        <input
          ref={input}
          type="datetime-local"
          required={required}
          value={wall}
          onChange={(e) => {
            const next = e.target.value;
            localEdit.current = true;
            setWall(next);
            try {
              const values = localCandidates(next, zone);
              onChange(values.length === 1 ? values[0]! : null);
            } catch {
              onChange(null);
            }
          }}
        />
      </label>
      {wall && !candidates.length && (
        <p role="alert">
          {error ||
            "Ten czas lokalny nie istnieje w wybranej strefie. Wskaż inny termin."}
        </p>
      )}
      {candidates.length > 1 && (
        <label>
          Wystąpienie czasu — {label}
          <select
            required
            value={selected}
            onChange={(e) => onChange(e.target.value || null)}
          >
            <option value="">Wybierz przed lub po zmianie czasu</option>
            {candidates.map((v) => (
              <option key={v} value={v}>
                {new Intl.DateTimeFormat("pl-PL", {
                  timeZone: zone,
                  dateStyle: "short",
                  timeStyle: "long",
                }).format(new Date(v))}{" "}
                · UTC {v}
              </option>
            ))}
          </select>
        </label>
      )}
      {value && (
        <small>
          {zone} · UTC {value}
        </small>
      )}
    </div>
  );
}
