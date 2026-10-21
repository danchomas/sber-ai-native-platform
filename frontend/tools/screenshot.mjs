// Снимает интерфейс на нескольких ширинах и сообщает о горизонтальной
// прокрутке. Нужен, чтобы вёрстку проверяли глазами, а не на веру.
//
//   node tools/screenshot.mjs [url] [каталог]

import { chromium } from "playwright";

const url = process.argv[2] || "http://localhost:3000/";
const out = process.argv[3] || "/tmp/shots";

const VIEWPORTS = [
  [1920, 1080, "desktop-1920"],
  [1440, 900, "desktop-1440"],
  [1024, 768, "tablet-1024"],
  [390, 844, "phone-390"],
];

const browser = await chromium.launch();
for (const [width, height, name] of VIEWPORTS) {
  const page = await browser.newPage({ viewport: { width, height } });
  await page.goto(url, { waitUntil: "networkidle", timeout: 30000 });
  await page.waitForTimeout(1200);
  await page.screenshot({ path: `${out}/${name}.png` });

  const m = await page.evaluate(() => ({
    scrollW: document.documentElement.scrollWidth,
    clientW: document.documentElement.clientWidth,
  }));
  const overflow = m.scrollW > m.clientW ? `ЕСТЬ (${m.scrollW} > ${m.clientW})` : "нет";
  console.log(`${name.padEnd(14)} горизонтальная прокрутка: ${overflow}`);
  await page.close();
}
await browser.close();
