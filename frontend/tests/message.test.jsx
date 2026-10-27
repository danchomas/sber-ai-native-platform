import { renderToString } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { AssistantMessage } from "../src/features/chat/components/Message.jsx";

const answer = {
  intent: "query",
  text: "Больше всего оплаченных заказов — 700.",
  query: { dataset: "orders", group_by: ["status"] },
  result: {
    columns: [{ name: "Статус", type: "text" }, { name: "Количество", type: "bigint" }],
    rows: [["paid", 700], ["new", 200]],
    row_count: 2,
  },
  chart: { type: "bar", title: "Заказы по статусам", x: "Статус", y: ["Количество"] },
  chart_options: [
    { type: "bar", title: "Заказы по статусам" },
    { type: "pie", title: "Заказы по статусам" },
    { type: "table", title: "Заказы по статусам" },
  ],
  steps: [
    { tool: "aggregate", title: "Посчитал показатели", ok: false, detail: "колонки «region» нет", elapsed_ms: 12 },
    { tool: "aggregate", title: "Посчитал показатели", ok: true, rows: 2, elapsed_ms: 30 },
  ],
};

const render = (a) =>
  renderToString(<AssistantMessage answer={a} theme="light" onAsk={() => {}} onOpenDashboard={() => {}} />);

describe("ответ агента", () => {
  it("показывает ход работы с исправленной ошибкой", () => {
    const html = render(answer);
    expect(html).toContain("Ход работы: 2 шага");
    expect(html).toContain("исправлено ошибок: 1");
    expect(html).toContain("колонки «region» нет");
  });

  it("предлагает переключить вид графика", () => {
    const html = render(answer);
    for (const label of ["Столбцы", "Круг", "Таблица"]) expect(html).toContain(label);
  });

  it("даёт закрепить расчёт на дашборде", () => {
    expect(render(answer)).toContain("Закрепить на дашборде");
    expect(render({ ...answer, intent: "clarify" })).not.toContain("Закрепить на дашборде");
  });
});
