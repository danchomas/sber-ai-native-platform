// Дымовой тест: приложение собирается в разметку без падений.
//
// Браузера здесь нет, поэтому проверяется первая отрисовка — она ловит
// ошибки импортов, неправильное применение хуков и падения компонентов.
// Эффекты при серверной отрисовке не выполняются, значит запросов к API
// не будет: тест не требует поднятой платформы.

import { renderToString } from "react-dom/server";
import { beforeAll, describe, expect, it } from "vitest";

import { App } from "../src/app/App.jsx";

beforeAll(() => {
  // Тема читает предпочтение системы и хранилище ещё до первого эффекта.
  globalThis.window ??= {};
  globalThis.window.matchMedia ??= () => ({ matches: false });
  globalThis.localStorage ??= { getItem: () => null, setItem() {} };
});

describe("каркас приложения", () => {
  const html = () => renderToString(<App />);

  it("отрисовывается без ошибок", () => {
    expect(html()).toContain("AI-Native BI");
  });

  it("показывает все разделы", () => {
    const markup = html();
    for (const title of ["Чат", "Данные", "Источники", "Дашборды"]) {
      expect(markup).toContain(title);
    }
  });

  it("встречает примерами вопросов, пока диалог пуст", () => {
    expect(html()).toContain("Что есть в этих данных");
  });
});
