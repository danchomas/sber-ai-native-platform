import { useEffect, useState } from "react";

import { api } from "../shared/api/PlatformApi.js";
import { truncate } from "../shared/lib/format.js";

const POLL_MS = 30000;

// useHealth опрашивает состояние платформы.
//
// Платформа штатно работает без языковой модели, поэтому degraded — это
// предупреждение, а не ошибка: отвечать на вопросы она всё ещё может.
export function useHealth() {
  const [health, setHealth] = useState({ tone: "warn", text: "проверка состояния…" });

  useEffect(() => {
    let alive = true;

    const poll = async () => {
      try {
        const h = await api.health();
        if (!alive) return;
        const llm = h.components?.llm || {};
        if (h.status === "ok") {
          setHealth({ tone: "ok", text: `модель: ${truncate(llm.detail || "подключена", 22)}`, details: h.components });
        } else if (h.status === "degraded") {
          setHealth({ tone: "warn", text: "модель недоступна", details: h.components });
        } else {
          setHealth({ tone: "down", text: "база данных недоступна", details: h.components });
        }
      } catch {
        if (alive) setHealth({ tone: "down", text: "сервер недоступен" });
      }
    };

    poll();
    const timer = setInterval(poll, POLL_MS);
    return () => {
      alive = false;
      clearInterval(timer);
    };
  }, []);

  return health;
}
