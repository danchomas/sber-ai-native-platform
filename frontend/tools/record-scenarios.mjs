import fs from "node:fs";
import path from "node:path";

import { chromium } from "playwright";

// Запись прогона вопросов ТЗ в видео: один ролик на сценарий, каждый сценарий — отдельный диалог.
// node tools/record-scenarios.mjs [адрес] [каталог] [сценарии через запятую] [параллельно]

const BASE = (process.argv[2] || "http://localhost:3000").replace(/\/$/, "");
const OUT = process.argv[3] || ".screenshots/videos";
const ONLY = process.argv[4] ? new Set(process.argv[4].split(",")) : null;
const PARALLEL = Number(process.argv[5] || 3);

const SCENARIOS = [
  ["tz1", "ТЗ-1", [[1, "Что передо мной?"], [2, "Что есть в этих данных?"], [3, "За какой период они и сколько их?"]]],
  ["tz2", "ТЗ-2", [[4, "Что означает поле «Код исходной позиции»?"], [5, "Какие значения встречаются чаще всего во всех колонках?"],
    [6, "Что связывает записи с одинаковым идентификатором колонки «Код заказа»?"]]],
  ["tz3", "ТЗ-3", [[7, "Что происходит с выручкой каждый июль?"], [8, "Есть ли тут повторяющиеся данные?"]]],
  ["tz4-6", "ТЗ-4 и ТЗ-6", [[9, "Покажи наглядно, как распределена выручка по категориям товаров"],
    [10, "А теперь покажи то же самое в другом виде"], [11, "Расскажи подробнее про второй пункт"]]],
  ["tz5", "ТЗ-5", [[12, "Создай дашборд с ежеквартальной статистикой продаж по федеральным округам"]]],
  ["tz7", "ТЗ-7", [[13, "Какой паттерн повторяется каждые 12 месяцев?"], [14, "Есть ли сезонные пики зимой?"]]],
  ["tz8", "ТЗ-8", [[15, "Как группируются покупатели по типу покупателя + уровню программы лояльности?"],
    [16, "Что отличает покупателей типа «Организация» от «Физическое лицо»?"]]],
  ["tz9", "ТЗ-9", [[17, "Изменения в размере скидки предшествуют росту количества проданных товаров на 5 дней?"]]],
  ["tz10", "ТЗ-10", [[18, "Какой рост выручки ожидается в ближайшие кварталы при текущих условиях?"],
    [19, "Как изменится выручка, если поднять цену товаров категории «Электроника» на 10%?"]]],
];

const VIEW = { width: 1440, height: 900 };
const results = [];

async function label(page, text) {
  await page.evaluate((t) => {
    let el = document.getElementById("run-label");
    if (!el) {
      el = document.createElement("div");
      el.id = "run-label";
      el.style.cssText = "position:fixed;right:16px;bottom:12px;z-index:99999;padding:6px 12px;border-radius:8px;" +
        "background:#111827;color:#fff;font:13px Inter,sans-serif;opacity:.85;pointer-events:none";
      document.body.appendChild(el);
    }
    el.textContent = t;
  }, text);
}

// Плавно проводит ответ через экран, чтобы его можно было прочитать на видео.
async function walkAnswer(page) {
  const box = await page.evaluate(() => {
    const users = document.querySelectorAll(".chat-view.active .msg.user");
    const last = users[users.length - 1];
    const scroll = document.querySelector(".chat-view.active .scroll");
    if (!last || !scroll) return null;
    scroll.scrollTop += last.getBoundingClientRect().top - scroll.getBoundingClientRect().top - 12;
    return { rest: scroll.scrollHeight - scroll.clientHeight - scroll.scrollTop };
  });
  await page.waitForTimeout(2500);
  if (!box) return;
  const steps = Math.min(Math.ceil(box.rest / 30), 200);
  for (let i = 0; i < steps; i++) {
    await page.evaluate(() => { document.querySelector(".chat-view.active .scroll").scrollTop += 30; });
    await page.waitForTimeout(90);
  }
  await page.waitForTimeout(2000);
}

async function run(browser, [id, title, questions]) {
  const dir = path.join(OUT, `.raw-${id}`);
  const ctx = await browser.newContext({ viewport: VIEW, recordVideo: { dir, size: VIEW } });
  const page = await ctx.newPage();
  const problems = [];
  page.on("pageerror", (e) => problems.push(String(e)));

  let sessionId = null;
  page.on("response", async (r) => {
    if (r.request().method() === "POST" && /\/api\/v1\/sessions$/.test(r.url()) && r.ok()) {
      try { sessionId = (await r.json()).id; } catch { /* ответ уже закрыт */ }
    }
  });

  await page.goto(BASE + "/", { waitUntil: "networkidle" });
  await page.locator(".new-chat").click();
  await page.waitForTimeout(800);

  for (const [num, question] of questions) {
    await label(page, `${title} · вопрос ${num} из 19`);
    const started = Date.now();
    const box = page.getByLabel("Вопрос к AI-аналитику");
    await box.pressSequentially(question, { delay: 25 });
    await page.waitForTimeout(400);
    await box.press("Enter");
    let timedOut = false;
    try {
      await page.locator(".send .spinner").waitFor({ state: "visible", timeout: 8000 }).catch(() => {});
      await page.locator(".send .spinner").waitFor({ state: "detached", timeout: 330000 });
    } catch { timedOut = true; }
    const elapsed = Date.now() - started;
    await page.waitForTimeout(900);
    await walkAnswer(page);

    const res = await page.request.get(`${BASE}/api/v1/sessions/${sessionId}/messages?limit=100`);
    const body = res.ok() ? await res.json() : { messages: [] };
    const last = (body.messages || []).at(-1);
    const answer = last?.answer ?? null;
    results.push({ video: id, title, num, question, session: sessionId, elapsed_ms: elapsed, timed_out: timedOut, answer });
    console.log(`${id} #${num} ${(elapsed / 1000).toFixed(0)}с intent=${answer?.intent} chart=${answer?.chart?.type ?? "-"} ` +
      `rows=${answer?.result?.rows?.length ?? "-"} ${timedOut ? "ТАЙМАУТ" : ""}`);

    if (answer?.dashboard_id) {
      const open = page.locator(".chat-view.active .msg .chips .chip", { hasText: "Открыть дашборд" }).last();
      if (await open.isVisible().catch(() => false)) await open.click();
      await page.waitForLoadState("networkidle").catch(() => {});
      await page.getByText("пересчитываю показатели").waitFor({ state: "detached", timeout: 180000 }).catch(() => {});
      await page.waitForTimeout(4000);
      await page.mouse.move(VIEW.width / 2, VIEW.height / 2);
      for (let i = 0; i < 40; i++) {
        await page.mouse.wheel(0, 40);
        await page.waitForTimeout(100);
      }
      await page.waitForTimeout(2000);
      await page.screenshot({ path: path.join(OUT, `${id}-dashboard.png`) });
    }
  }

  const video = page.video();
  await ctx.close();
  fs.renameSync(await video.path(), path.join(OUT, `${id}.webm`));
  fs.rmSync(dir, { recursive: true, force: true });
  if (problems.length) console.log(`${id} ошибки страницы: ${problems.join(" | ")}`);
  fs.writeFileSync(path.join(OUT, `results-${ONLY ? [...ONLY].join("_") : "all"}.json`),
    JSON.stringify(results.toSorted((a, b) => a.num - b.num), null, 1));
}

(async () => {
  fs.mkdirSync(OUT, { recursive: true });
  // На старых macOS Playwright не ставит свой браузер: CHROME_PATH указывает на уже установленный.
  const browser = await chromium.launch({ executablePath: process.env.CHROME_PATH || undefined });
  const queue = SCENARIOS.filter(([id]) => !ONLY || ONLY.has(id));
  await Promise.all(Array.from({ length: PARALLEL }, async () => {
    for (let next = queue.shift(); next; next = queue.shift()) {
      try { await run(browser, next); } catch (e) { console.error("СБОЙ", next[0], e); }
    }
  }));
  await browser.close();
  console.log("ГОТОВО");
})();
