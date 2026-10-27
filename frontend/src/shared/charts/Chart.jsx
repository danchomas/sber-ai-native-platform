import { useEffect, useRef } from "react";

import { render, tableView } from "./viz.js";
import "./charts.css";

export function Chart({ spec, result, height = 300, theme }) {
  const host = useRef(null);

  useEffect(() => {
    const node = host.current;
    if (!node) return;
    node.replaceChildren(render(spec || { type: "table" }, result, { height }));
  }, [spec, result, height, theme]);

  return <div ref={host} />;
}

export function Table({ result, limit = 25, theme }) {
  const host = useRef(null);

  useEffect(() => {
    const node = host.current;
    if (!node) return;
    node.replaceChildren(tableView(result, limit));
  }, [result, limit, theme]);

  return <div ref={host} />;
}
