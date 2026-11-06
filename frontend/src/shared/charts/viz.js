import { h, s } from "../lib/dom.js";
import { fmtCell, fmtCompact, fmtDate, fmtNum, isNum, toNum, truncate } from "../lib/format.js";
import { plural } from "../lib/plural.js";

const W = 760;
const MARK_MAX = 24;
const GAP = 2;

function tokens() {
  const cs = getComputedStyle(document.documentElement);
  const read = (name) => cs.getPropertyValue(name).trim();
  return {
    series: [1, 2, 3, 4, 5, 6, 7, 8].map((i) => read(`--series-${i}`)),
    grid: read("--grid"),
    axis: read("--axis"),
    surface: read("--surface-1"),
    muted: read("--text-muted"),
    seqFrom: read("--seq-100"),
    seqTo: read("--seq-700"),
  };
}

const categoryColors = new Map();

function colorFor(value, palette) {
  const key = String(value);
  if (!categoryColors.has(key)) categoryColors.set(key, categoryColors.size);
  return palette[categoryColors.get(key) % palette.length];
}

function colIndex(result, name) {
  if (!name) return -1;
  return result.columns.findIndex((c) => c.name === name);
}

function isNumericCol(col) {
  return col && (col.type === "bigint" || col.type === "double precision");
}

function periodKey(label) {
  let m = /^([1-4]) кв\. (\d{4})$/.exec(label);
  if (m) return Number(m[2]) * 100 + Number(m[1]) * 3;
  m = /^(\d{2})\.(\d{4})$/.exec(label);
  if (m) return Number(m[2]) * 100 + Number(m[1]);
  return null;
}

function arePeriods(labels) {
  return labels.length > 1 && labels.every((l) => periodKey(l) !== null);
}

function periodLabel(result, index, value) {
  const col = result.columns[index];
  const m = isTemporalCol(col) ? /^(\d{4})-(\d{2})-(\d{2})/.exec(String(value ?? "")) : null;
  if (!m) return fmtCell(value);
  const [, year, month, day] = m;
  if (col.name === "Квартал") return `${Math.ceil(Number(month) / 3)} кв. ${year}`;
  if (col.name === "Месяц") return `${month}.${year}`;
  if (col.name === "Год") return year;
  return `${day}.${month}.${year}`;
}

function shape(spec, result) {
  const xi = colIndex(result, spec.x);
  const si = colIndex(result, spec.series);
  const yNames = (spec.y || []).filter((n) => colIndex(result, n) >= 0);
  const palette = tokens().series;

  if (si >= 0 && yNames.length >= 1) {
    const yi = colIndex(result, yNames[0]);
    const cats = [];
    const names = [];
    const cells = new Map();
    for (const row of result.rows) {
      const cat = periodLabel(result, xi, row[xi]);
      const name = fmtCell(row[si]);
      if (!cats.includes(cat)) cats.push(cat);
      if (!names.includes(name)) names.push(name);
      cells.set(`${cat}\u0000${name}`, toNum(row[yi]));
    }
    if (arePeriods(cats)) cats.sort((a, b) => periodKey(a) - periodKey(b));
    return {
      categories: cats,
      series: names.slice(0, 8).map((name) => ({
        name,
        color: colorFor(name, palette),
        values: cats.map((cat) => cells.get(`${cat}\u0000${name}`) ?? null),
      })),
    };
  }

  let rows = result.rows;
  if (xi >= 0 && arePeriods(rows.map((row) => periodLabel(result, xi, row[xi])))) {
    rows = [...rows].sort((a, b) => periodKey(periodLabel(result, xi, a[xi])) - periodKey(periodLabel(result, xi, b[xi])));
  }
  const cats = rows.map((row, i) => (xi >= 0 ? periodLabel(result, xi, row[xi]) : String(i + 1)));
  const names = yNames.length ? yNames : result.columns.filter(isNumericCol).map((c) => c.name);
  return {
    categories: cats,
    series: names.slice(0, 8).map((name, i) => {
      const idx = colIndex(result, name);
      return {
        name,
        color: palette[i % palette.length],
        values: rows.map((row) => toNum(row[idx])),
      };
    }),
  };
}

function niceCeil(value) {
  if (value <= 0) return 1;
  const exp = Math.floor(Math.log10(value));
  const base = Math.pow(10, exp);
  for (const m of [1, 1.5, 2, 2.5, 3, 4, 5, 7.5, 10]) {
    if (value <= m * base) return m * base;
  }
  return 10 * base;
}

function extent(series) {
  let min = 0, max = 0;
  for (const sr of series) for (const v of sr.values) {
    if (v === null) continue;
    if (v < min) min = v;
    if (v > max) max = v;
  }
  if (max === 0 && min === 0) max = 1;
  return { min: min < 0 ? -niceCeil(-min) : 0, max: niceCeil(max) };
}

function roundedRect(x, y, w, hgt, r, side) {
  const rad = Math.max(0, Math.min(r, w / 2, hgt / 2));
  if (side === "top") {
    return `M${x},${y + hgt}V${y + rad}a${rad},${rad} 0 0 1 ${rad},${-rad}h${w - 2 * rad}a${rad},${rad} 0 0 1 ${rad},${rad}V${y + hgt}Z`;
  }
  if (side === "bottom") {
    return `M${x},${y}V${y + hgt - rad}a${rad},${rad} 0 0 0 ${rad},${rad}h${w - 2 * rad}a${rad},${rad} 0 0 0 ${rad},${-rad}V${y}Z`;
  }
  if (side === "right") {
    return `M${x},${y}h${w - rad}a${rad},${rad} 0 0 1 ${rad},${rad}v${hgt - 2 * rad}a${rad},${rad} 0 0 1 ${-rad},${rad}H${x}Z`;
  }
  return `M${x},${y}h${w}v${hgt}H${x}Z`;
}

function axisY(svg, scale, box, t, formatter) {
  const ticks = 5;
  for (let i = 0; i <= ticks; i++) {
    const value = scale.min + ((scale.max - scale.min) * i) / ticks;
    const y = box.y + box.h - ((value - scale.min) / (scale.max - scale.min)) * box.h;
    s("line", { x1: box.x, y1: y, x2: box.x + box.w, y2: y, stroke: t.grid, "stroke-width": 1 }, svg);
    s("text", { x: box.x - 8, y: y + 4, "text-anchor": "end", text: fmtCompact(value) }, svg);
  }
}

const LABEL_ANGLE = 35;
const LABEL_CHAR_W = 6.5;

function rotatesLabels(categories, width) {
  const band = width / Math.max(1, categories.length);
  const longest = categories.reduce((m, c) => Math.max(m, String(c).length), 0);
  return longest > 10 && band < 70;
}

// labelOverflow — сколько высоты добавить под осью X, чтобы наклонные подписи
// не уходили за нижний край графика и не налезали на легенду. room — место,
// которое уже есть между осью и краем.
function labelOverflow(categories, width, room) {
  if (!rotatesLabels(categories, width)) return 0;
  const chars = Math.min(categories.reduce((m, c) => Math.max(m, String(c).length), 0), 18);
  const depth = 12 + chars * LABEL_CHAR_W * Math.sin((LABEL_ANGLE * Math.PI) / 180) + 6;
  return Math.max(0, Math.ceil(depth - room));
}

function axisXLabels(svg, categories, box, t) {
  const step = Math.ceil(categories.length / Math.max(1, Math.floor(box.w / 64)));
  const band = box.w / categories.length;
  const rotate = rotatesLabels(categories, box.w);

  categories.forEach((cat, i) => {
    if (i % step !== 0) return;
    const cx = box.x + band * (i + 0.5);
    const label = truncate(cat, rotate ? 18 : 14);
    if (rotate) {
      s("text", {
        x: cx, y: box.y + box.h + 12, "text-anchor": "end",
        transform: `rotate(-${LABEL_ANGLE} ${cx} ${box.y + box.h + 12})`, text: label,
      }, svg);
    } else {
      s("text", { x: cx, y: box.y + box.h + 16, "text-anchor": "middle", text: label }, svg);
    }
  });
}

const TIP_SEP = "\n";

function tipText(head, value) {
  return `${head}${TIP_SEP}${value}`;
}

function showTip(tip, text) {
  const [head, ...rest] = String(text).split(TIP_SEP);
  tip.replaceChildren(
    document.createTextNode(head),
    ...rest.flatMap((line) => [h("br"), h("b", { text: line })]),
  );
}

function attachTip(svg, tip) {
  svg.addEventListener("mousemove", (ev) => {
    const target = ev.target.closest("[data-tip]");
    if (!target) { tip.style.opacity = 0; return; }
    const rect = svg.parentElement.getBoundingClientRect();
    showTip(tip, target.dataset.tip);
    tip.style.opacity = 1;
    const left = Math.min(Math.max(ev.clientX - rect.left + 12, 0), rect.width - tip.offsetWidth - 4);
    tip.style.left = `${left}px`;
    tip.style.top = `${Math.max(ev.clientY - rect.top - tip.offsetHeight - 10, 0)}px`;
  });
  svg.addEventListener("mouseleave", () => { tip.style.opacity = 0; });
}

function columns(data, spec, t, height) {
  const box = { x: 58, y: 12, w: W - 74, h: height - 58 };
  const H = height + labelOverflow(data.categories, box.w, height - box.y - box.h);
  const svg = s("svg", { viewBox: `0 0 ${W} ${H}`, role: "img" });
  const stacked = !!spec.stacked && data.series.length > 1;

  const scale = stacked
    ? { min: 0, max: niceCeil(Math.max(...data.categories.map((_, i) =>
        data.series.reduce((sum, sr) => sum + (sr.values[i] || 0), 0)))) }
    : extent(data.series);

  axisY(svg, scale, box, t);
  const zeroY = box.y + box.h - ((0 - scale.min) / (scale.max - scale.min)) * box.h;
  s("line", { x1: box.x, y1: zeroY, x2: box.x + box.w, y2: zeroY, stroke: t.axis, "stroke-width": 1 }, svg);

  const band = box.w / data.categories.length;
  const count = stacked ? 1 : data.series.length;
  const markW = Math.min(MARK_MAX, Math.max(3, (band - GAP * (count + 1)) / count));
  const scaleY = (v) => (Math.abs(v) / (scale.max - scale.min)) * box.h;

  data.categories.forEach((cat, i) => {
    let stackTop = zeroY;
    data.series.forEach((sr, j) => {
      const value = sr.values[i];
      if (value === null) return;
      const len = Math.max(scaleY(value), 1);
      const x = stacked
        ? box.x + band * i + (band - markW) / 2
        : box.x + band * i + GAP + j * (markW + GAP) + (band - GAP - count * (markW + GAP)) / 2;

      let y, side;
      if (stacked) {
        y = stackTop - len;
        stackTop = y - GAP;
        side = j === data.series.length - 1 ? "top" : "flat";
      } else {
        y = value >= 0 ? zeroY - len : zeroY;
        side = value >= 0 ? "top" : "bottom";
      }

      s("path", {
        d: roundedRect(x, y, markW, len, 4, side),
        fill: sr.color,
        dataset: { tip: tipText(truncate(cat, 40), `${sr.name}: ${fmtNum(value)}`) },
      }, svg);

      if (!stacked && data.series.length === 1 && data.categories.length <= 12 && value >= 0) {
        s("text", { x: x + markW / 2, y: y - 6, "text-anchor": "middle", class: "value", text: fmtCompact(value) }, svg);
      }
    });
  });

  axisXLabels(svg, data.categories, box, t);
  return svg;
}

function bars(data, spec, t, height) {
  const rows = data.categories.length;
  const H = Math.max(height, 34 * rows + 40);
  const box = { x: 148, y: 10, w: W - 190, h: H - 34 };
  const svg = s("svg", { viewBox: `0 0 ${W} ${H}`, role: "img" });
  const scale = extent(data.series);

  const ticks = 4;
  for (let i = 0; i <= ticks; i++) {
    const value = scale.min + ((scale.max - scale.min) * i) / ticks;
    const x = box.x + ((value - scale.min) / (scale.max - scale.min)) * box.w;
    s("line", { x1: x, y1: box.y, x2: x, y2: box.y + box.h, stroke: t.grid, "stroke-width": 1 }, svg);
    s("text", { x, y: box.y + box.h + 15, "text-anchor": "middle", text: fmtCompact(value) }, svg);
  }
  s("line", { x1: box.x, y1: box.y, x2: box.x, y2: box.y + box.h, stroke: t.axis, "stroke-width": 1 }, svg);

  const band = box.h / rows;
  const count = data.series.length;
  const markH = Math.min(MARK_MAX, Math.max(3, (band - GAP * (count + 1)) / count));

  data.categories.forEach((cat, i) => {
    s("text", {
      x: box.x - 10, y: box.y + band * (i + 0.5) + 4,
      "text-anchor": "end", text: truncate(cat, 22),
    }, svg);

    data.series.forEach((sr, j) => {
      const value = sr.values[i];
      if (value === null) return;
      const len = Math.max((Math.abs(value) / (scale.max - scale.min)) * box.w, 1);
      const y = box.y + band * i + GAP + j * (markH + GAP) + (band - GAP - count * (markH + GAP)) / 2;

      s("path", {
        d: roundedRect(box.x, y, len, markH, 4, "right"),
        fill: sr.color,
        dataset: { tip: tipText(truncate(cat, 40), `${sr.name}: ${fmtNum(value)}`) },
      }, svg);

      if (count === 1 && len + 46 < box.w) {
        s("text", { x: box.x + len + 7, y: y + markH / 2 + 4, class: "value", text: fmtCompact(value) }, svg);
      }
    });
  });
  return svg;
}

function lines(data, spec, t, height, area) {
  const box = { x: 58, y: 12, w: W - 130, h: height - 58 };
  const H = height + labelOverflow(data.categories, box.w, height - box.y - box.h);
  const svg = s("svg", { viewBox: `0 0 ${W} ${H}`, role: "img" });
  const scale = extent(data.series);
  axisY(svg, scale, box, t);

  const n = Math.max(data.categories.length, 1);
  const px = (i) => box.x + (n === 1 ? box.w / 2 : (box.w * i) / (n - 1));
  const py = (v) => box.y + box.h - ((v - scale.min) / (scale.max - scale.min)) * box.h;

  data.series.forEach((sr) => {
    const points = sr.values.map((v, i) => (v === null ? null : [px(i), py(v)])).filter(Boolean);
    if (!points.length) return;
    const last = sr.values.reduce((acc, v, i) => (v === null ? acc : i), -1);
    // Последний период неполный: его отрезок пунктиром, точка полая —
    // обрыв линии в конце выгрузки не должен читаться как перемена.
    const partial = Boolean(spec.partial_last) && points.length >= 2 && last === data.categories.length - 1;

    if (area) {
      const d = `M${points[0][0]},${box.y + box.h}` +
        points.map((p) => `L${p[0]},${p[1]}`).join("") +
        `L${points[points.length - 1][0]},${box.y + box.h}Z`;
      s("path", { d, fill: sr.color, "fill-opacity": 0.1 }, svg);
    }

    const solid = partial ? points.slice(0, -1) : points;
    s("path", {
      d: "M" + solid.map((p) => `${p[0]},${p[1]}`).join("L"),
      fill: "none", stroke: sr.color, "stroke-width": 2,
      "stroke-linejoin": "round", "stroke-linecap": "round",
    }, svg);
    if (partial) {
      const [a, b] = points.slice(-2);
      s("path", {
        d: `M${a[0]},${a[1]}L${b[0]},${b[1]}`, class: "partial",
        fill: "none", stroke: sr.color, "stroke-width": 2, "stroke-dasharray": "5 5",
        "stroke-linecap": "round", "stroke-opacity": 0.7,
      }, svg);
    }

    if (points.length <= 40) {
      sr.values.forEach((v, i) => {
        if (v === null) return;
        const unfinished = partial && i === last;
        s("circle", {
          cx: px(i), cy: py(v), r: 4, fill: unfinished ? t.surface : sr.color,
          stroke: unfinished ? sr.color : t.surface, "stroke-width": 2,
          dataset: {
            tip: tipText(truncate(data.categories[i], 40),
              `${sr.name}: ${fmtNum(v)}${unfinished ? " — период неполный" : ""}`),
          },
        }, svg);
      });
    }

    if (last >= 0 && data.series.length <= 4) {
      s("text", {
        x: px(last) + 7, y: py(sr.values[last]) + 4,
        class: "value", text: fmtCompact(sr.values[last]) + (partial ? " · неполный" : ""),
      }, svg);
    }
  });

  axisXLabels(svg, data.categories, box, t);
  return svg;
}

function pie(data, spec, t, height, donut) {
  const H = Math.max(height, 300);
  const svg = s("svg", { viewBox: `0 0 ${W} ${H}`, role: "img" });
  const cx = W / 2, cy = H / 2;
  const radius = Math.min(H / 2 - 16, 130);
  const inner = donut ? radius * 0.58 : 0;

  const sr = data.series[0];
  if (!sr) return svg;
  const total = sr.values.reduce((sum, v) => sum + Math.max(v || 0, 0), 0);
  if (total <= 0) return svg;

  const palette = tokens().series;
  let angle = -Math.PI / 2;

  data.categories.forEach((cat, i) => {
    const value = Math.max(sr.values[i] || 0, 0);
    if (value <= 0) return;
    const share = value / total;
    const end = angle + share * Math.PI * 2;
    const large = end - angle > Math.PI ? 1 : 0;

    const x0 = cx + radius * Math.cos(angle), y0 = cy + radius * Math.sin(angle);
    const x1 = cx + radius * Math.cos(end), y1 = cy + radius * Math.sin(end);
    let d = `M${x0},${y0}A${radius},${radius} 0 ${large} 1 ${x1},${y1}`;
    if (inner) {
      const xi1 = cx + inner * Math.cos(end), yi1 = cy + inner * Math.sin(end);
      const xi0 = cx + inner * Math.cos(angle), yi0 = cy + inner * Math.sin(angle);
      d += `L${xi1},${yi1}A${inner},${inner} 0 ${large} 0 ${xi0},${yi0}Z`;
    } else {
      d += `L${cx},${cy}Z`;
    }

    s("path", {
      d, fill: colorFor(cat, palette),
      stroke: t.surface, "stroke-width": GAP,
      dataset: { tip: tipText(truncate(cat, 40), `${fmtNum(value)} · ${(share * 100).toFixed(1)}%`) },
    }, svg);

    if (share >= 0.05) {
      const mid = (angle + end) / 2;
      const lr = inner ? (radius + inner) / 2 : radius * 0.68;
      s("text", {
        x: cx + lr * Math.cos(mid), y: cy + lr * Math.sin(mid) + 4,
        "text-anchor": "middle", class: "value", fill: "#fff",
        text: `${Math.round(share * 100)}%`,
      }, svg);
    }
    angle = end;
  });

  if (donut) {
    s("text", { x: cx, y: cy - 2, "text-anchor": "middle", class: "value", text: "всего" }, svg);
    s("text", {
      x: cx, y: cy + 20, "text-anchor": "middle", class: "value",
      style: "font-size:18px;font-weight:650", text: fmtCompact(total),
    }, svg);
  }
  return svg;
}

function scatter(data, spec, result, t, height) {
  const H = height;
  const box = { x: 58, y: 12, w: W - 74, h: H - 52 };
  const svg = s("svg", { viewBox: `0 0 ${W} ${H}`, role: "img" });

  const xi = colIndex(result, spec.x);
  const yi = colIndex(result, (spec.y || [])[0]);
  if (xi < 0 || yi < 0) return svg;

  const points = result.rows
    .map((row) => [toNum(row[xi]), toNum(row[yi])])
    .filter((p) => p[0] !== null && p[1] !== null);
  if (!points.length) return svg;

  const xs = points.map((p) => p[0]), ys = points.map((p) => p[1]);
  const xMin = Math.min(...xs), xMax = Math.max(...xs);
  const yScale = { min: Math.min(0, Math.min(...ys)), max: niceCeil(Math.max(...ys)) };
  axisY(svg, yScale, box, t);

  const px = (v) => box.x + (xMax === xMin ? box.w / 2 : ((v - xMin) / (xMax - xMin)) * box.w);
  const py = (v) => box.y + box.h - ((v - yScale.min) / (yScale.max - yScale.min)) * box.h;
  const color = tokens().series[0];

  for (let i = 0; i <= 4; i++) {
    const value = xMin + ((xMax - xMin) * i) / 4;
    s("text", { x: px(value), y: box.y + box.h + 16, "text-anchor": "middle", text: fmtCompact(value) }, svg);
  }

  points.forEach(([x, y]) => {
    s("circle", {
      cx: px(x), cy: py(y), r: 4.5, fill: color, "fill-opacity": 0.85,
      stroke: t.surface, "stroke-width": 2,
      dataset: { tip: tipText(`${spec.x}: ${fmtNum(x)}`, `${(spec.y || [])[0]}: ${fmtNum(y)}`) },
    }, svg);
  });
  return svg;
}

function histogram(spec, result, t, height) {
  const target = spec.x && colIndex(result, spec.x) >= 0
    ? spec.x
    : (result.columns.find(isNumericCol) || {}).name;
  const idx = colIndex(result, target);
  if (idx < 0) return null;

  const values = result.rows.map((row) => toNum(row[idx])).filter((v) => v !== null);
  if (!values.length) return null;

  const min = Math.min(...values), max = Math.max(...values);
  const binCount = Math.min(20, Math.max(5, Math.round(Math.sqrt(values.length))));
  const width = (max - min) / binCount || 1;
  const counts = new Array(binCount).fill(0);
  for (const v of values) {
    counts[Math.min(binCount - 1, Math.floor((v - min) / width))]++;
  }

  const data = {
    categories: counts.map((_, i) => `${fmtCompact(min + i * width)}–${fmtCompact(min + (i + 1) * width)}`),
    series: [{ name: "Количество записей", color: tokens().series[0], values: counts }],
  };
  return columns(data, { stacked: false }, t, height);
}

function heatmap(data, spec, t, height) {
  const rows = data.series.length;
  const base = Math.max(height, 26 * rows + 70);
  const box = { x: 148, y: 10, w: W - 170, h: 24 * rows };
  const H = base + labelOverflow(data.categories, box.w, base - box.y - box.h);
  const svg = s("svg", { viewBox: `0 0 ${W} ${H}`, role: "img" });

  let max = 0;
  for (const sr of data.series) for (const v of sr.values) if (v !== null && v > max) max = v;
  if (max <= 0) max = 1;

  const band = box.w / data.categories.length;
  data.series.forEach((sr, r) => {
    s("text", { x: box.x - 10, y: box.y + 24 * r + 17, "text-anchor": "end", text: truncate(sr.name, 22) }, svg);
    sr.values.forEach((v, c) => {
      if (v === null) return;
      s("rect", {
        x: box.x + band * c + 1, y: box.y + 24 * r + 1,
        width: Math.max(band - GAP, 1), height: 22, rx: 3,
        fill: t.seqTo, "fill-opacity": 0.12 + 0.88 * (v / max),
        dataset: { tip: tipText(`${truncate(data.categories[c], 30)} · ${truncate(sr.name, 30)}`, fmtNum(v)) },
      }, svg);
    });
  });

  axisXLabels(svg, data.categories, { ...box, h: box.h }, t);
  return svg;
}

function isTemporalCol(col) {
  return col && /^(date|timestamp)/.test(col.type);
}

function kpis(spec, result) {
  const names = (spec.y || []).filter((n) => colIndex(result, n) >= 0);
  const metrics = names.length ? names : result.columns.filter(isNumericCol).map((c) => c.name);
  const dates = result.columns.filter((c) => isTemporalCol(c) && !metrics.includes(c.name)).map((c) => c.name);
  const row = result.rows[0] || [];

  return h("div", { class: "kpis" }, [...dates, ...metrics].slice(0, 6).map((name) => {
    const idx = colIndex(result, name);
    const value = row[idx];
    const isDate = isTemporalCol(result.columns[idx]);
    return h("div", { class: "kpi" }, [
      h("div", { class: "label", text: name }),
      h("div", { class: isDate ? "value is-date" : "value", text: isDate ? fmtDate(value) : fmtCompact(value) }),
    ]);
  }));
}

function tableView(result, limit) {
  result = { ...result, rows: result?.rows ?? [], columns: result?.columns ?? [] };
  const rows = result.rows.slice(0, limit || 200);
  return h("div", { class: "table-wrap" }, [
    h("table", { class: "data" }, [
      h("thead", null, h("tr", null, result.columns.map((c) => h("th", { text: c.name, title: c.name })))),
      h("tbody", null, rows.map((row) => h("tr", null, row.map((cell, i) => h("td", {
        class: isNumericCol(result.columns[i]) ? "num" : "",
        text: periodLabel(result, i, cell),
        title: periodLabel(result, i, cell),
      }))))),
    ]),
  ]);
}

function legend(data) {
  if (data.series.length < 2) return null;
  return h("div", { class: "legend" }, data.series.map((sr) =>
    h("span", null, [h("i", { style: `background:${sr.color}` }), truncate(sr.name, 28)])));
}

function categoryLegend(data) {
  const palette = tokens().series;
  return h("div", { class: "legend" }, data.categories.slice(0, 8).map((cat) =>
    h("span", null, [h("i", { style: `background:${colorFor(cat, palette)}` }), truncate(cat, 28)])));
}

function render(spec, result, options) {
  result = { ...result, rows: result?.rows ?? [], columns: result?.columns ?? [] };
  const opts = options || {};
  const height = opts.height || 300;
  const type = (spec && spec.type) || "table";

  if (!result || !result.columns || !result.columns.length) {
    return h("p", { class: "muted", text: "Нет данных для отображения." });
  }
  if (!result.rows.length) {
    return h("p", { class: "muted", text: "Запрос не вернул ни одной строки." });
  }

  const t = tokens();
  const data = shape(spec || {}, result);
  let plot = null;
  let plotLegend = null;

  switch (type) {
    case "kpi": plot = kpis(spec, result); break;
    case "bar": plot = columns(data, spec, t, height); plotLegend = legend(data); break;
    case "hbar": plot = bars(data, spec, t, height); plotLegend = legend(data); break;
    case "line": plot = lines(data, spec, t, height, false); plotLegend = legend(data); break;
    case "area": plot = lines(data, spec, t, height, true); plotLegend = legend(data); break;
    case "pie": plot = pie(data, spec, t, height, false); plotLegend = categoryLegend(data); break;
    case "donut": plot = pie(data, spec, t, height, true); plotLegend = categoryLegend(data); break;
    case "scatter": plot = scatter(data, spec, result, t, height); break;
    case "histogram": plot = histogram(spec, result, t, height); break;
    case "heatmap": plot = heatmap(data, spec, t, height); plotLegend = null; break;
    default: plot = null;
  }

  const table = tableView(result, opts.tableLimit);
  const figure = h("figure", { class: "viz" });

  const title = (spec && spec.title) || opts.title || "";
  const sub = result.row_count
    ? `${fmtNum(result.row_count)} ${plural(result.row_count, "строка", "строки", "строк")}`
    : "";

  const caption = h("figcaption", null, [
    title ? h("h4", { text: truncate(title, 90), title }) : null,
    sub ? h("span", { class: "sub", text: sub }) : null,
    h("span", { class: "spacer" }),
  ]);

  if (plot) {
    const body = h("div", { class: "viz-body" });
    const tip = h("div", { class: "tip" });
    body.appendChild(plot);
    body.appendChild(tip);
    if (plot.tagName === "svg") attachTip(plot, tip);

    const tableBlock = h("div", { style: "display:none;margin-top:12px" }, table);
    const toggle = h("button", { class: "icon-btn", text: "Таблица" });
    toggle.addEventListener("click", () => {
      const shown = tableBlock.style.display !== "none";
      tableBlock.style.display = shown ? "none" : "block";
      toggle.textContent = shown ? "Таблица" : "Скрыть таблицу";
      toggle.classList.toggle("active", !shown);
    });
    caption.appendChild(toggle);

    figure.append(caption, body);
    if (plotLegend) figure.appendChild(plotLegend);
    figure.appendChild(tableBlock);
  } else {
    figure.append(caption, table);
  }
  return figure;
}

export { render, tableView, showTip, tipText };
