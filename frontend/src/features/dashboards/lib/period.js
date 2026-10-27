// Быстрые фильтры периода считаются от последней даты в данных панели,
// а не от сегодняшнего дня: исторический датасет за 2023 год при фильтре
// «последний квартал от сегодня» показал бы пустые графики.

const PRESETS = [
  { id: "all", label: "Весь период", months: 0 },
  { id: "year", label: "Последний год", months: 12 },
  { id: "half", label: "Полгода", months: 6 },
  { id: "quarter", label: "Квартал", months: 3 },
  { id: "month", label: "Месяц", months: 1 },
];

function iso(d) {
  return d.toISOString().slice(0, 10);
}

// presetRange возвращает {from, to} для быстрого фильтра или null для «весь период».
function presetRange(preset, range) {
  if (!preset.months || !range?.to) return null;
  const to = new Date(`${range.to}T00:00:00Z`);
  const from = new Date(to);
  from.setUTCMonth(from.getUTCMonth() - preset.months);
  from.setUTCDate(from.getUTCDate() + 1);
  const start = range.from && iso(from) < range.from ? range.from : iso(from);
  return { from: start, to: range.to };
}

function samePeriod(a, b) {
  return (a?.from || "") === (b?.from || "") && (a?.to || "") === (b?.to || "");
}

function fmtDay(value) {
  if (!value) return "";
  const [y, m, d] = value.split("-");
  return `${d}.${m}.${y}`;
}

export { PRESETS, presetRange, samePeriod, fmtDay };
