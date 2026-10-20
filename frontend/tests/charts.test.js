// Движок визуализаций перенесён из встроенного интерфейса целиком.
// Тест проверяет, что перенос не сломал его: по форме результата он
// выдаёт нужный тип разметки и всегда даёт табличную проекцию.

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
  it("рисует столбцы по категории и метрике (тип bar)", () => {
    const rs = resultSet(
      [{ name: "Статус", type: "text" }, { name: "Количество", type: "bigint" }],
      [["оплачен", 700], ["отменён", 180], ["новый", 120]],
    );
    const node = render({ type: "bar", x: "Статус", y: ["Количество"] }, rs, { height: 260 });

    expect(node.querySelector("svg")).toBeTruthy();
    // Столбцы рисуются скруглёнными path, а не rect.
    expect(node.querySelectorAll("path").length).toBeGreaterThan(0);
  });

  it("единственное число показывает как показатель", () => {
    const rs = resultSet([{ name: "Выручка", type: "double precision" }], [[1234567]]);
    const node = render({ type: "kpi", y: ["Выручка"] }, rs, {});

    expect(node.textContent).toContain("Выручка");
  });

  it("к любому графику прикладывает таблицу", () => {
    const rs = resultSet(
      [{ name: "Месяц", type: "date" }, { name: "Сумма", type: "double precision" }],
      [["2024-01-01", 10], ["2024-02-01", 20]],
    );
    const node = render({ type: "line", x: "Месяц", y: ["Сумма"] }, rs, { height: 240 });

    // Табличная проекция обязательна: она же страхует контраст в светлой теме.
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
