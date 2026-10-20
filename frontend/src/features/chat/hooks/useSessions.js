import { useCallback, useEffect, useState } from "react";

import { api } from "../../../shared/api/PlatformApi.js";

// useSessions ведёт список диалогов и активный диалог.
//
// Память платформы живёт на уровне сессии, поэтому активный диалог —
// это и есть контекст, в котором агент отвечает.
export function useSessions() {
  const [items, setItems] = useState([]);
  const [activeId, setActiveId] = useState(null);
  const [error, setError] = useState(null);

  const reload = useCallback(async () => {
    try {
      const list = await api.listSessions();
      setItems(list);
      setError(null);
      return list;
    } catch (err) {
      setError(err);
      return [];
    }
  }, []);

  const create = useCallback(async () => {
    const session = await api.createSession();
    setActiveId(session.id);
    await reload();
    return session;
  }, [reload]);

  // При первом открытии продолжаем последний диалог, а если их нет — заводим.
  useEffect(() => {
    let alive = true;
    (async () => {
      const list = await reload();
      if (!alive) return;
      if (list.length) setActiveId(list[0].id);
      else {
        try {
          const s = await api.createSession();
          if (alive) {
            setActiveId(s.id);
            reload();
          }
        } catch (err) {
          if (alive) setError(err);
        }
      }
    })();
    return () => {
      alive = false;
    };
  }, [reload]);

  const active = items.find((s) => s.id === activeId) || null;

  return { items, activeId, active, error, select: setActiveId, create, reload };
}
