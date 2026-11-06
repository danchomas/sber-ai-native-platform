import fs from "node:fs";
import path from "node:path";

import { chromium } from "playwright";

const BASE = (process.argv[2] || "http://localhost:3000").replace(/\/$/, "");
const OUT = process.argv[3] || ".screenshots/scenarios";
const ONLY = process.argv[4] ? new Set(process.argv[4].split(",").map(Number)) : null;

const GROUPS = [
  [[1, "Что передо мной?"], [2, "Что есть в этих данных?"], [3, "За какой период они и сколько их?"]],
  [[4, "Что означает поле «Код исходной позиции»?"], [5, "Какие значения встречаются чаще всего во всех колонках?"],
   [6, "Что связывает записи с одинаковым идентификатором колонки «Код заказа»?"]],
  [[7, "Что происходит с выручкой каждый июль?"], [8, "Есть ли тут повторяющиеся данные?"]],
  [[9, "Покажи наглядно, как распределена выручка по категориям товаров"], [10, "А теперь покажи то же самое в другом виде"],
   [11, "Расскажи подробнее про второй пункт"]],
  [[12, "Создай дашборд с ежеквартальной статистикой продаж по федеральным округам"]],
  [[13, "Какой паттерн повторяется каждые 12 месяцев?"], [14, "Есть ли сезонные пики зимой?"]],
  [[15, "Как группируются покупатели по типу покупателя + уровню программы лояльности?"],
   [16, "Что отличает покупателей типа «Организация» от «Физическое лицо»?"]],
  [[17, "Изменения в размере скидки предшествуют росту количества проданных товаров на 5 дней?"]],
  [[18, "Какой рост выручки ожидается в ближайшие кварталы при текущих условиях?"],
   [19, "Как изменится выручка, если поднять цену товаров категории «Электроника» на 10%?"]],
];

const VIEW = { width: 1600, height: 1000 };

// scrollToAnswerEnd ставит конец последнего ответа у нижнего края ленты.
// false — ответ и так поместился в кадр.
const scrollToAnswerEnd = (page) => page.evaluate(() => {
  const scroll = document.querySelector(".chat-view.active .scroll");
  const msgs = document.querySelectorAll(".chat-view.active .msg");
  const last = msgs[msgs.length - 1];
  if (!scroll || !last) return false;
  const overflow = last.getBoundingClientRect().bottom - scroll.getBoundingClientRect().bottom + 16;
  if (overflow <= 0) return false;
  scroll.scrollTop += overflow;
  return true;
});

// scrollPageEnd прокручивает до конца самую большую прокручиваемую область
// страницы (у панели это её содержимое, а не окно).
const scrollPageEnd = (page) => page.evaluate(() => {
  const areas = [document.scrollingElement, ...document.querySelectorAll("*")].filter((el) => el
    && el.scrollHeight > el.clientHeight + 40
    && (el === document.scrollingElement || /(auto|scroll)/.test(getComputedStyle(el).overflowY)));
  const area = areas.sort((a, b) => b.clientHeight - a.clientHeight)[0];
  if (!area) return false;
  area.scrollTop = area.scrollHeight;
  return true;
});
const pad = (n) => String(n).padStart(2, "0");

(async () => {
  fs.mkdirSync(OUT, { recursive: true });
  const browser = await chromium.launch();
  const ctx = await browser.newContext({ viewport: VIEW, deviceScaleFactor: 2, acceptDownloads: true });
  const page = await ctx.newPage();
  const problems = [];
  page.on("pageerror", (e) => problems.push({ kind: "pageerror", text: String(e) }));
  page.on("console", (m) => { if (m.type() === "error") problems.push({ kind: "console", text: m.text() }); });

  let sessionId = null;
  page.on("response", async (r) => {
    if (r.request().method() === "POST" && /\/api\/v1\/sessions$/.test(r.url()) && r.ok()) {
      try { sessionId = (await r.json()).id; } catch {}
    }
  });

  await page.goto(BASE + "/", { waitUntil: "networkidle" });
  const results = [];

  for (const group of GROUPS) {
    const todo = ONLY ? group.filter(([n]) => ONLY.has(n)) : group;
    if (!todo.length) continue;
    sessionId = null;
    await page.locator(".new-chat").click();
    await page.waitForTimeout(600);
    for (const [num, question] of group) {
      if (ONLY && !ONLY.has(num)) continue;
      const started = Date.now();
      const box = page.getByLabel("Вопрос к AI-аналитику");
      await box.fill(question);
      await box.press("Enter");
      let timedOut = false;
      try {
        await page.locator(".send .spinner").waitFor({ state: "visible", timeout: 8000 }).catch(() => {});
        await page.locator(".send .spinner").waitFor({ state: "detached", timeout: 330000 });
      } catch { timedOut = true; }
      await page.waitForTimeout(900);
      const elapsed = Date.now() - started;

      // Вопрос наверху видимой области — как на эталонных скриншотах.
      await page.evaluate(() => {
        const users = document.querySelectorAll(".chat-view.active .msg.user");
        const last = users[users.length - 1];
        const scroll = document.querySelector(".chat-view.active .scroll");
        if (last && scroll) scroll.scrollTop += last.getBoundingClientRect().top - scroll.getBoundingClientRect().top - 12;
      });
      await page.waitForTimeout(300);
      await page.screenshot({ path: path.join(OUT, `${pad(num)}.png`) });

      // Конец ответа — вторым кадром того же размера, а не одним высоким:
      // окно в 3200 пикселей давало снимки 1:2, непригодные для слайдов.
      if (await scrollToAnswerEnd(page)) {
        await page.waitForTimeout(300);
        await page.screenshot({ path: path.join(OUT, `${pad(num)}_more.png`) });
      }

      const res = await page.request.get(`${BASE}/api/v1/sessions/${sessionId}/messages?limit=100`);
      const body = res.ok() ? await res.json() : { messages: [] };
      const msgs = body.messages || [];
      const last = msgs[msgs.length - 1];
      const answer = last && last.answer ? last.answer : null;
      results.push({ num, question, session: sessionId, elapsed_ms: elapsed, timed_out: timedOut, answer, text: last ? last.content : null });
      console.log(`${pad(num)} ${(elapsed / 1000).toFixed(1)}s intent=${answer && answer.intent} rows=${answer && answer.result ? (answer.result.rows || []).length : "-"} chart=${answer && answer.chart ? answer.chart.type : "-"} analysis=${answer && answer.analysis || "-"} :: ${question}`);
      fs.writeFileSync(path.join(OUT, "results.json"), JSON.stringify({ results, problems }, null, 1));

      if (answer && answer.dashboard_id) {
        const open = page.locator(".chat-view.active .msg .chips .chip", { hasText: "Открыть дашборд" }).last();
        if (await open.isVisible().catch(() => false)) await open.click();
        await page.waitForLoadState("networkidle").catch(() => {});
        await page.waitForTimeout(2500);
        await page.screenshot({ path: path.join(OUT, `${pad(num)}_dashboard.png`) });
        if (await scrollPageEnd(page)) {
          await page.waitForTimeout(500);
          await page.screenshot({ path: path.join(OUT, `${pad(num)}_dashboard_more.png`) });
        }
        await page.locator(".nav button", { hasText: "Чат" }).click();
        await page.waitForTimeout(400);
      }
    }
  }
  fs.writeFileSync(path.join(OUT, "results.json"), JSON.stringify({ results, problems }, null, 1));
  console.log(`DONE problems=${problems.length}`);
  await browser.close();
})().catch((e) => { console.error("FATAL", e); process.exit(1); });
