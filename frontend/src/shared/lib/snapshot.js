const XHTML = "http://www.w3.org/1999/xhtml";
const SVG = "http://www.w3.org/2000/svg";
const MAX_CANVAS_SIDE = 16000;

const NOT_IN_SNAPSHOT = ".widget-tools, .ds-actions, .period-bar, .title-line .icon-btn, .viz figcaption .icon-btn";

function inlineStyles(source, clone) {
  const computed = getComputedStyle(source);
  let css = "";
  for (let i = 0; i < computed.length; i += 1) {
    const name = computed[i];
    css += `${name}:${computed.getPropertyValue(name)};`;
  }
  clone.setAttribute("style", `${css}animation:none;transition:none;`);

  const sourceKids = [...source.children];
  const cloneKids = [...clone.children];
  sourceKids.forEach((kid, i) => {
    if (kid.matches(NOT_IN_SNAPSHOT)) cloneKids[i].remove();
    else inlineStyles(kid, cloneKids[i]);
  });
}

function loadImage(url) {
  return new Promise((resolve, reject) => {
    const image = new Image();
    image.onload = () => resolve(image);
    image.onerror = () => reject(new Error("Браузер не смог собрать картинку."));
    image.src = url;
  });
}

function pageBackground() {
  return getComputedStyle(document.documentElement).getPropertyValue("--page").trim() || "#ffffff";
}

function usedFontRules() {
  const loaded = [...document.fonts].filter((f) => f.status === "loaded");
  const rules = [];
  for (const sheet of document.styleSheets) {
    let list = [];
    try {
      list = [...sheet.cssRules];
    } catch {
      continue;
    }
    for (const rule of list) {
      if (rule.type !== CSSRule.FONT_FACE_RULE) continue;
      const weight = rule.style.getPropertyValue("font-weight");
      const range = rule.style.getPropertyValue("unicode-range").replace(/\s/g, "").toLowerCase();
      const used = loaded.some((f) => f.weight === weight && f.unicodeRange.replace(/\s/g, "").toLowerCase() === range);
      if (used) rules.push({ css: rule.cssText, base: sheet.href || location.href });
    }
  }
  return rules;
}

function toDataUrl(blob) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(reader.result);
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(blob);
  });
}

async function embeddedFonts() {
  const parts = await Promise.all(usedFontRules().map(async ({ css, base }) => {
    const match = /url\(["']?([^"')]+)["']?\)(?:\s*format\([^)]*\))?/.exec(css);
    if (!match) return "";
    const response = await fetch(new URL(match[1], base));
    if (!response.ok) return "";
    const data = await toDataUrl(await response.blob());
    return css.replace(/src:[^;]+;/, `src: url("${data}") format("woff2");`);
  }));
  return parts.join("\n");
}

export async function snapshotPng(node, { padding = 24, scale = 2 } = {}) {
  const { width, height } = node.getBoundingClientRect();
  const clone = node.cloneNode(true);
  inlineStyles(node, clone);
  clone.setAttribute("xmlns", XHTML);
  clone.style.margin = "0";

  const fullWidth = Math.ceil(width) + 2 * padding;
  const fullHeight = Math.ceil(height) + 2 * padding;
  const svg = document.createElementNS(SVG, "svg");
  svg.setAttribute("width", fullWidth);
  svg.setAttribute("height", fullHeight);
  const fonts = document.createElementNS(SVG, "style");
  fonts.textContent = await embeddedFonts().catch(() => "");
  svg.append(fonts);
  const frame = document.createElementNS(SVG, "foreignObject");
  frame.setAttribute("x", padding);
  frame.setAttribute("y", padding);
  frame.setAttribute("width", Math.ceil(width));
  frame.setAttribute("height", Math.ceil(height));
  frame.append(clone);
  svg.append(frame);

  const markup = new XMLSerializer().serializeToString(svg);
  const image = await loadImage(`data:image/svg+xml;charset=utf-8,${encodeURIComponent(markup)}`);

  const ratio = Math.min(scale, MAX_CANVAS_SIDE / Math.max(fullWidth, fullHeight));
  const canvas = document.createElement("canvas");
  canvas.width = Math.floor(fullWidth * ratio);
  canvas.height = Math.floor(fullHeight * ratio);
  const ctx = canvas.getContext("2d");
  ctx.fillStyle = pageBackground();
  ctx.fillRect(0, 0, canvas.width, canvas.height);
  ctx.scale(ratio, ratio);
  ctx.drawImage(image, 0, 0);

  return new Promise((resolve, reject) => {
    canvas.toBlob((blob) => (blob ? resolve(blob) : reject(new Error("Браузер не смог собрать картинку."))), "image/png");
  });
}

export function imageFileName(title) {
  const name = String(title || "dashboard").replace(/[\\/:*?"<>|\u0000-\u001f]/g, "_").trim();
  return `${name || "dashboard"}.png`;
}
