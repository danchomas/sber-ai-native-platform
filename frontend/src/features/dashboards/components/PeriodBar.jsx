import { useEffect, useState } from "react";

import { PRESETS, fmtDay, presetRange, samePeriod } from "../lib/period.js";

export function PeriodBar({ range, period, onChange }) {
  const [from, setFrom] = useState(period?.from || "");
  const [to, setTo] = useState(period?.to || "");

  useEffect(() => {
    setFrom(period?.from || "");
    setTo(period?.to || "");
  }, [period?.from, period?.to]);

  if (!range) return null;

  const custom = period && !PRESETS.some((p) => samePeriod(presetRange(p, range), period));
  const invalid = from && to && to < from;

  return (
    <div className="period-bar" role="group" aria-label="Период">
      <div className="chips">
        {PRESETS.map((preset) => {
          const value = presetRange(preset, range);
          const active = preset.months ? samePeriod(value, period) : !period;
          return (
            <button
              key={preset.id}
              className={`chip${active ? " active" : ""}`}
              aria-pressed={active}
              onClick={() => onChange(value)}
            >
              {preset.label}
            </button>
          );
        })}
      </div>
      <form
        className={`period-custom${custom ? " active" : ""}`}
        onSubmit={(e) => {
          e.preventDefault();
          if (!invalid) onChange(from || to ? { from, to } : null);
        }}
      >
        <label>
          <span>с</span>
          <input type="date" value={from} min={range.from} max={range.to} onChange={(e) => setFrom(e.target.value)} />
        </label>
        <label>
          <span>по</span>
          <input type="date" value={to} min={range.from} max={range.to} onChange={(e) => setTo(e.target.value)} />
        </label>
        <button className="icon-btn" type="submit" disabled={invalid}>
          Применить
        </button>
      </form>
      <span className="period-hint">
        {`Данные: ${fmtDay(range.from)} — ${fmtDay(range.to)}`}
      </span>
    </div>
  );
}
