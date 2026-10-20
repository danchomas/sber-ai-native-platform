import { useCallback, useEffect, useState } from "react";

import { api } from "../../../shared/api/PlatformApi.js";

// useDatasets ведёт каталог: и загруженные файлы, и подключённые таблицы
// внешних источников — для интерфейса это один список.
export function useDatasets() {
  const [items, setItems] = useState([]);
  const [relations, setRelations] = useState([]);
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(true);

  const reload = useCallback(async () => {
    setLoading(true);
    try {
      const [list, rels] = await Promise.all([api.listDatasets(), api.relations()]);
      setItems(list);
      setRelations(rels);
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

  const remove = useCallback(
    async (id) => {
      await api.deleteDataset(id);
      await reload();
    },
    [reload],
  );

  const upload = useCallback(
    async (file) => {
      const report = await api.uploadDataset(file);
      await reload();
      return report;
    },
    [reload],
  );

  return { items, relations, error, loading, reload, remove, upload };
}
