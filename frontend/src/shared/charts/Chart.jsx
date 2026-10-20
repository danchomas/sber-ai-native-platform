import { useEffect, useRef } from "react";

import { render, tableView } from "./viz.js";
import "./charts.css";

// Chart отдаёт движку визуализаций кусок DOM под управление.
//
// Движок рисует SVG императивно, поэтому React не трогает его поддерево:
// при смене данных узел пересоздаётся целиком. Тема входит в зависимости —
// цвета читаются из CSS-токенов в момент отрисовки.
export function Chart({ spec, result, height = 300, theme }) {
  const host = useRef(null);

  useEffect(() => {
    const node = host.current;
    if (!node) return;
    node.replaceChildren(render(spec || { type: "table" }, result, { height }));
  }, [spec, result, height, theme]);

  return <div ref={host} />;
}

// Table показывает результат запроса таблицей — без графика.
export function Table({ result, limit = 25, theme }) {
  const host = useRef(null);

  useEffect(() => {
    const node = host.current;
    if (!node) return;
    node.replaceChildren(tableView(result, limit));
  }, [result, limit, theme]);

  return <div ref={host} />;
}
