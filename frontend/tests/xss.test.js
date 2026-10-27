// Значения из CSV и подписи, которые выбрала модель, попадают в подсказки
// графиков. Подсказка обязана показывать их текстом: строка с HTML из
// данных не должна стать разметкой и тем более выполниться.

// @vitest-environment happy-dom

import { describe, expect, it } from "vitest";

import { render, showTip } from "../src/shared/charts/viz.js";

const payload = `<img src=x onerror="window.__pwned=1">`;

describe("подсказки графиков", () => {
  it("значение из данных остаётся текстом", () => {
    const rs = {
      columns: [{ name: "Город", type: "text" }, { name: payload, type: "bigint" }],
      rows: [[payload, 10], ["Москва", 5]],
      row_count: 2,
      truncated: false,
    };
    const node = render({ type: "bar", x: "Город", y: [payload] }, rs, { height: 240 });
    const mark = node.querySelector("[data-tip]");
    expect(mark).toBeTruthy();

    const tip = document.createElement("div");
    showTip(tip, mark.dataset.tip);

    expect(tip.querySelector("img")).toBeNull();
    expect(tip.textContent).toContain(payload);
    expect(window.__pwned).toBeUndefined();
  });

  it("ни один график не вставляет разметку из данных", () => {
    const rs = {
      columns: [{ name: "Категория", type: "text" }, { name: "Сумма", type: "double precision" }],
      rows: [[payload, 1], ["b", 2]],
      row_count: 2,
      truncated: false,
    };
    for (const type of ["bar", "hbar", "line", "pie", "donut", "heatmap", "table"]) {
      const node = render({ type, x: "Категория", y: ["Сумма"] }, rs, { height: 240 });
      expect(node.querySelector("img")).toBeNull();
    }
  });
});
