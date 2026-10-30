// Форматирование чисел, дат и размеров для интерфейса.
// Локаль фиксирована: платформа русскоязычная.

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

// fmtDate — день без времени: для периода данных время только мешает.
// Время из загруженных файлов хранится как есть, в UTC, — переводить его
// в пояс браузера нельзя: 23:54 30 сентября стало бы 1 октября.
function fmtDate(v) {
  if (v === null || v === undefined || v === "") return "—";
  const d = new Date(v);
  if (Number.isNaN(d.getTime())) return String(v);
  return d.toLocaleDateString("ru-RU", { day: "2-digit", month: "2-digit", year: "numeric", timeZone: "UTC" });
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

export { isNum, toNum, fmtNum, fmtCompact, fmtCell, fmtDate, fmtDateTime, fmtBytes, truncate };
