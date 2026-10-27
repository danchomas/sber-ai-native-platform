const numberFmt = new Intl.NumberFormat("ru-RU", { maximumFractionDigits: 2 });
const compactFmt = new Intl.NumberFormat("ru-RU", { notation: "compact", maximumFractionDigits: 1 });

function isNum(v) {
  return typeof v === "number" && Number.isFinite(v);
}

function toNum(v) {
  if (isNum(v)) return v;
  if (typeof v === "string" && v.trim() !== "") {
    const n = Number(v.replace(/\s/g, "").replace(",", "."));
    return Number.isFinite(n) ? n : null;
  }
  return null;
}

function fmtNum(v) {
  const n = toNum(v);
  return n === null ? "—" : numberFmt.format(n);
}

function fmtCompact(v) {
  const n = toNum(v);
  if (n === null) return "—";
  return Math.abs(n) >= 10000 ? compactFmt.format(n) : numberFmt.format(n);
}

function fmtCell(v) {
  if (v === null || v === undefined) return "—";
  if (isNum(v)) return numberFmt.format(v);
  return String(v);
}

function fmtDateTime(iso) {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return String(iso);
  return d.toLocaleString("ru-RU", { day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit" });
}

function fmtBytes(n) {
  if (!n) return "—";
  const units = ["Б", "КБ", "МБ", "ГБ"];
  let i = 0, v = n;
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
  return `${numberFmt.format(Math.round(v * 10) / 10)} ${units[i]}`;
}

function truncate(text, max) {
  const str = String(text ?? "");
  return str.length > max ? str.slice(0, max - 1) + "…" : str;
}

export { isNum, toNum, fmtNum, fmtCompact, fmtCell, fmtDateTime, fmtBytes, truncate };
