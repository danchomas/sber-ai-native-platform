import { useCallback, useEffect, useState } from "react";

import { api } from "../../../shared/api/PlatformApi.js";

// useSources ведёт подключённые базы и их таблицы.
//
// Подключение проверяется платформой до записи в каталог, поэтому
// неудачная строка подключения просто возвращает ошибку и ничего не создаёт.
export function useSources() {
  const [items, setItems] = useState([]);
  const [tables, setTables] = useState({});
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(true);

  const reload = useCallback(async () => {
    setLoading(true);
    try {
      setItems(await api.listSources());
      setError(null);
    } catch (err) {
      setError(err);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    reload();
  }, [reload]);

  const attach = useCallback(
    async (name, dsn) => {
      const source = await api.attachSource(name, dsn);
      await reload();
      return source;
    },
    [reload],
  );

  const loadTables = useCallback(async (id) => {
    const list = await api.sourceTables(id);
    setTables((prev) => ({ ...prev, [id]: list }));
    return list;
  }, []);

  const attachTable = useCallback(
    async (id, schema, table) => {
      const dataset = await api.attachTable(id, schema, table);
      await loadTables(id);
      return dataset;
    },
    [loadTables],
  );

  const detach = useCallback(
    async (id) => {
      await api.detachSource(id);
      setTables((prev) => {
        const next = { ...prev };
        delete next[id];
        return next;
      });
      await reload();
    },
    [reload],
  );

  return { items, tables, error, loading, reload, attach, loadTables, attachTable, detach };
}
