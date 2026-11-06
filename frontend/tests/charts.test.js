// @vitest-environment happy-dom

import { describe, expect, it } from "vitest";

import { render, tableView } from "../src/shared/charts/viz.js";

const resultSet = (columns, rows) => ({
  columns,
  rows,
  row_count: rows.length,
  truncated: false,
});

describe("движок визуализаций", () => {
  it("подписывает кварталы и месяцы по-русски, а не датой начала периода", () => {
    const rs = resultSet(
      [{ name: "Квартал", type: "date" }, { name: "Выручка", type: "double precision" }],
      [["2026-07-01", 3], ["2024-10-01", 1], ["2025-01-01", 2]],
    );
    const text = render({ type: "bar", x: "Квартал", y: ["Выручка"] }, rs, { height: 260 }).textContent;
    expect(text).toContain("4 кв. 2024");
    expect(text).not.toContain("2024-10-01");

    const table = tableView(resultSet([{ name: "Месяц", type: "date" }], [["2025-03-01"]]), 10);
    expect(table.textContent).toContain("03.2025");
  });

  it("не падает на пустом результате без строк (rows: null)", () => {
    const rs = { columns: [{ name: "Код", type: "text" }], rows: null, row_count: 0 };

    expect(render({ type: "table" }, rs, { height: 260 }).textContent).toContain("ни одной строки");
    expect(tableView(rs, 10).querySelector("table")).toBeTruthy();
  });

  it("рисует столбцы по категории и метрике (тип bar)", () => {
    const rs = resultSet(
      [{ name: "Статус", type: "text" }, { name: "Количество", type: "bigint" }],
      [["оплачен", 700], ["отменён", 180], ["новый", 120]],
    );
    const node = render({ type: "bar", x: "Статус", y: ["Количество"] }, rs, { height: 260 });

    expect(node.querySelector("svg")).toBeTruthy();
    expect(node.querySelectorAll("path").length).toBeGreaterThan(0);
  });

  it("неполный последний период рисует пунктиром и подписывает", () => {
    const rs = resultSet(
      [{ name: "Неделя", type: "date" }, { name: "Длительность, ч", type: "double precision" }],
      [["2026-09-14", 44.3], ["2026-09-21", 35.7], ["2026-09-28", 18.8]],
    );
    const spec = { type: "line", x: "Неделя", y: ["Длительность, ч"] };
    const plain = render(spec, rs, { height: 260 });
    expect(plain.querySelector("path.partial")).toBeNull();

    const node = render({ ...spec, partial_last: true }, rs, { height: 260 });
    expect(node.querySelector("path.partial")?.getAttribute("stroke-dasharray")).toBe("5 5");
    expect(node.textContent).toContain("неполный");
  });

  it("единственное число показывает как показатель", () => {
    const rs = resultSet([{ name: "Выручка", type: "double precision" }], [[1234567]]);
    const node = render({ type: "kpi", y: ["Выручка"] }, rs, {});

    expect(node.textContent).toContain("Выручка");
  });

  it("в строке агрегатов показывает и даты периода, а не только числа", () => {
    const rs = resultSet(
      [
        { name: "Начало периода", type: "date" },
        { name: "Конец периода", type: "date" },
        { name: "Всего строк", type: "bigint" },
      ],
      [["2024-10-01T01:43:40Z", "2026-09-30T23:54:36Z", 500000]],
    );
    const node = render({ type: "kpi", y: ["Всего строк"] }, rs, {});

    expect(node.querySelectorAll(".kpi").length).toBe(3);
    expect(node.textContent).toContain("Начало периода");
    expect(node.textContent).toContain("01.10.2024");
    expect(node.textContent).toContain("30.09.2026");
  });

  it("одна категория — один цвет во всех графиках страницы", () => {
    const root = document.documentElement.style;
    ["#111111", "#222222", "#333333", "#444444", "#555555", "#666666", "#777777", "#888888"]
      .forEach((c, i) => root.setProperty(`--series-${i + 1}`, c));

    const legendColor = (node, name) => {
      const chip = [...node.querySelectorAll(".legend span")].find((el) => el.textContent === name);
      return chip?.querySelector("i")?.getAttribute("style");
    };

    const line = render(
      { type: "line", x: "Квартал", y: ["Выручка"], series: "Округ" },
      resultSet(
        [{ name: "Квартал", type: "text" }, { name: "Округ", type: "text" }, { name: "Выручка", type: "bigint" }],
        [["Q1", "Южный", 1], ["Q1", "Центральный", 5], ["Q2", "Южный", 2], ["Q2", "Центральный", 6]],
      ),
      { height: 240 },
    );
    const donut = render(
      { type: "donut", x: "Округ", y: ["Выручка"] },
      resultSet(
        [{ name: "Округ", type: "text" }, { name: "Выручка", type: "bigint" }],
        [["Центральный", 11], ["Южный", 3]],
      ),
      { height: 240 },
    );

    expect(legendColor(line, "Центральный")).toBeTruthy();
    expect(legendColor(donut, "Центральный")).toBe(legendColor(line, "Центральный"));
    expect(legendColor(donut, "Южный")).toBe(legendColor(line, "Южный"));
    expect(legendColor(donut, "Южный")).not.toBe(legendColor(donut, "Центральный"));
  });

  it("подписи периодов на оси идут по времени, а не по алфавиту", () => {
    const rows = [["1 кв. 2025", 10], ["1 кв. 2026", 30], ["2 кв. 2025", 15], ["4 кв. 2024", 5]];
    const node = render(
      { type: "line", x: "Квартал", y: ["Выручка"] },
      resultSet([{ name: "Квартал", type: "text" }, { name: "Выручка", type: "bigint" }], rows),
      { height: 240 },
    );
    const axis = [...node.querySelectorAll("svg text")].map((t) => t.textContent);
    const order = ["4 кв. 2024", "1 кв. 2025", "2 кв. 2025", "1 кв. 2026"].map((l) => axis.indexOf(l));
    expect(order.every((i) => i >= 0)).toBe(true);
    expect([...order].sort((a, b) => a - b)).toEqual(order);
  });

  it("под наклонные подписи оси добавляет высоту, не сжимая область графика", () => {
    const columns = [
      { name: "Значение", type: "text" },
      { name: "А", type: "double precision" },
      { name: "Б", type: "double precision" },
    ];
    const short = resultSet(columns, [["prod", 6, 8], ["test", 7, 15]]);
    const long = resultSet(columns, Array.from({ length: 12 }, (_, i) =>
      [`Хранилище данных ${i}`, 6, 8]));
    const viewHeight = (rs) => Number(render({ type: "bar", x: "Значение", y: ["А", "Б"] }, rs, { height: 260 })
      .querySelector("svg").getAttribute("viewBox").split(" ")[3]);

    expect(viewHeight(short)).toBe(260);
    expect(viewHeight(long)).toBeGreaterThan(260);
  });

  it("к любому графику прикладывает таблицу", () => {
    const rs = resultSet(
      [{ name: "Месяц", type: "date" }, { name: "Сумма", type: "double precision" }],
      [["2024-01-01", 10], ["2024-02-01", 20]],
    );
    const node = render({ type: "line", x: "Месяц", y: ["Сумма"] }, rs, { height: 240 });

    expect(node.querySelector("table")).toBeTruthy();
  });

  it("пустой результат не роняет отрисовку", () => {
    const node = render({ type: "table" }, resultSet([], []), {});
    expect(node).toBeTruthy();
  });

  it("таблица показывает заголовки колонок", () => {
    const rs = resultSet([{ name: "Клиент", type: "text" }], [["Альфа"], ["Бета"]]);
    const node = tableView(rs, 10);

    expect(node.textContent).toContain("Клиент");
    expect(node.textContent).toContain("Альфа");
  });
});
