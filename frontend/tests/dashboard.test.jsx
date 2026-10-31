import { renderToString } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { PeriodBar } from "../src/features/dashboards/components/PeriodBar.jsx";
import { Summary } from "../src/features/dashboards/components/Summary.jsx";
import { Widget } from "../src/features/dashboards/components/Widget.jsx";
import { PRESETS, presetRange } from "../src/features/dashboards/lib/period.js";
import { AssistantMessage } from "../src/features/chat/components/Message.jsx";
import { filenameFrom } from "../src/shared/api/HttpClient.js";

const trend = {
  id: "w1",
  title: "Выручка по месяцам",
  width: 12,
  chart: { type: "line", x: "Месяц", y: ["Выручка"] },
  result: {
    columns: [{ name: "Месяц", type: "date" }, { name: "Выручка", type: "double precision" }],
    rows: [["2024-05-01", 100], ["2024-06-01", 150]],
    row_count: 2,
  },
  headline: { label: "Последнее значение · 06.2024", value: 150, change_pct: 50, text: "" },
  period_applied: true,
};

const byStatus = {
  id: "w2",
  title: "Заказы по статусам",
  width: 6,
  chart: { type: "bar", x: "Статус", y: ["Количество"] },
  result: {
    columns: [{ name: "Статус", type: "text" }, { name: "Количество", type: "bigint" }],
    rows: [["paid", 700], ["new", 300]],
    row_count: 2,
  },
  headline: { label: "Лидер · paid", value: 700, share_pct: 70, text: "" },
};

describe("дашборд", () => {
  it("виджет показывает заголовок, главное число и изменение", () => {
    const html = renderToString(<Widget widget={trend} theme="light" />);
    expect(html).toContain("Выручка по месяцам");
    expect(html).toContain("Последнее значение · 06.2024");
    expect(html).toContain("delta up");
  });

  it("виджет без динамики помечает, что фильтр периода к нему не применён", () => {
    expect(renderToString(<Widget widget={byStatus} theme="light" filtered />)).toContain("за весь период");
    expect(renderToString(<Widget widget={trend} theme="light" filtered />)).not.toContain("за весь период");
  });

  it("ошибка виджета показывается вместо графика", () => {
    const html = renderToString(<Widget widget={{ ...byStatus, error: "не удалось пересчитать показатель" }} theme="light" />);
    expect(html).toContain("не удалось пересчитать показатель");
    expect(html).not.toContain("Лидер");
  });

  it("полоса «Главное» собирает числа виджетов", () => {
    const html = renderToString(<Summary widgets={[trend, byStatus]} />);
    expect(html).toContain("Главное на панели");
    expect(html).toContain("Заказы по статусам");
    expect(renderToString(<Summary widgets={[trend]} />)).toBe("");
  });

  it("быстрые фильтры считаются от последней даты данных", () => {
    const range = { from: "2023-01-01", to: "2024-06-30" };
    const quarter = PRESETS.find((p) => p.id === "quarter");
    expect(presetRange(quarter, range)).toEqual({ from: "2024-03-31", to: "2024-06-30" });
    expect(presetRange(PRESETS[0], range)).toBeNull();
    const html = renderToString(<PeriodBar range={range} period={null} onChange={() => {}} />);
    expect(html).toContain("Весь период");
    expect(html).toContain("01.01.2023 — 30.06.2024");
  });

  it("без данных о периоде фильтр не показывается", () => {
    expect(renderToString(<PeriodBar range={null} period={null} onChange={() => {}} />)).toBe("");
  });
});

describe("защита в ответе", () => {
  it("пометка о сработавшей защите видна пользователю", () => {
    const html = renderToString(
      <AssistantMessage
        answer={{ intent: "reject", text: "Не могу.", security: ["Вопрос не передавался языковой модели."] }}
        theme="light"
        onAsk={() => {}}
        onOpenDashboard={() => {}}
      />,
    );
    expect(html).toContain("notice security");
    expect(html).toContain("Вопрос не передавался языковой модели.");
  });

  it("виджет предлагает выгрузку в CSV, только когда она передана", () => {
    expect(renderToString(<Widget widget={trend} theme="light" onExport={() => {}} />)).toContain("Выгрузить виджет «Выручка по месяцам» в CSV");
    expect(renderToString(<Widget widget={trend} theme="light" />)).not.toContain("widget-export");
  });

  it("имя файла выгрузки читается из Content-Disposition", () => {
    const cd = `attachment; filename="dashboard.xlsx"; filename*=UTF-8''${encodeURIComponent("Продажи — квартал.xlsx")}`;
    expect(filenameFrom(cd)).toBe("Продажи — квартал.xlsx");
    expect(filenameFrom('attachment; filename="dashboard.csv"')).toBe("dashboard.csv");
    expect(filenameFrom(null)).toBeNull();
  });
});
