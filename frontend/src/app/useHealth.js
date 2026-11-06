import { useEffect, useState } from "react";

import { api } from "../shared/api/PlatformApi.js";
import { truncate } from "../shared/lib/format.js";

const POLL_MS = 30000;
// Повторная проверка после первого сбоя: один медленный ответ сети —
// не повод показывать «модель недоступна», если она только что отвечала.
const RETRY_MS = 5000;

export function useHealth() {
  const [health, setHealth] = useState({ tone: "warn", text: "проверка состояния…" });

  useEffect(() => {
    let alive = true;
    let seenOk = false;
    let failures = 0;
    let retry = null;

    const check = async () => {
      try {
        const h = await api.health();
        const llm = h.components?.llm || {};
        if (h.status === "ok") {
          return { tone: "ok", text: `модель: ${truncate(llm.detail || "подключена", 22)}`, details: h.components };
        }
        if (h.status === "degraded") {
          return { tone: "warn", text: "модель недоступна", details: h.components };
        }
        return { tone: "down", text: "база данных недоступна", details: h.components };
      } catch {
        return { tone: "down", text: "сервер недоступен" };
      }
    };

    const poll = async () => {
      const next = await check();
      if (!alive) return;
      if (next.tone === "ok") {
        seenOk = true;
        failures = 0;
        setHealth(next);
        return;
      }
      failures += 1;
      if (seenOk && failures === 1) {
        clearTimeout(retry);
        retry = setTimeout(poll, RETRY_MS);
        return;
      }
      setHealth(next);
    };

    poll();
    const timer = setInterval(poll, POLL_MS);
    return () => {
      alive = false;
      clearInterval(timer);
      clearTimeout(retry);
    };
  }, []);

  return health;
}
