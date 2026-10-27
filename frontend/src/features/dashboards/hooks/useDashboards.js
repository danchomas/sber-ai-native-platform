import { useCallback, useEffect, useState } from "react";

import { api } from "../../../shared/api/PlatformApi.js";

// useDashboards ведёт список панелей и открытую панель.
//
// Панель хранит вопросы и SQL, а не выгрузку, поэтому при открытии
// запросы выполняются заново — цифры всегда свежие.
export function useDashboards({ openId, onCount } = {}) {
  const [items, setItems] = useState([]);
  const [current, setCurrent] = useState(null);
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(false);

  const reload = useCallback(async () => {
    try {
      const list = await api.listDashboards();
      setItems(list);
      onCount?.(list.length);
      setError(null);
      return list;
    } catch (err) {
      setError(err);
      return [];
    }
  }, [onCount]);

  useEffect(() => {
    reload();
  }, [reload]);

  const [version, setVersion] = useState(0);

  useEffect(() => {
    if (!openId) {
      setCurrent(null);
      return;
    }
    let alive = true;
    setLoading(true);
    api
      .dashboard(openId, true)
      .then((d) => alive && setCurrent(d))
      .catch((err) => alive && setError(err))
      .finally(() => alive && setLoading(false));
    return () => {
      alive = false;
    };
  }, [openId, version]);

  const refresh = useCallback(() => setVersion((v) => v + 1), []);

  const removeWidget = useCallback(
    async (dashboardId, widgetId) => {
      await api.removeWidget(dashboardId, widgetId);
      setCurrent((d) => (d && d.id === dashboardId ? { ...d, widgets: d.widgets.filter((w) => w.id !== widgetId) } : d));
    },
    [],
  );

  const rename = useCallback(
    async (id, title) => {
      await api.renameDashboard(id, title);
      setCurrent((d) => (d && d.id === id ? { ...d, title } : d));
      await reload();
    },
    [reload],
  );

  const remove = useCallback(
    async (id) => {
      await api.deleteDashboard(id);
      await reload();
    },
    [reload],
  );

  return { items, current, error, loading, reload, refresh, remove, removeWidget, rename };
}
