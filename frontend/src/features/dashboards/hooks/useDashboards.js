import { useCallback, useEffect, useState } from "react";

import { api } from "../../../shared/api/PlatformApi.js";
import { saveBlob } from "../../../shared/lib/save.js";
import { imageFileName, snapshotPng } from "../../../shared/lib/snapshot.js";

const UNDO_SECONDS = 10;

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
  }, [reload, openId]);

  const [trash, setTrash] = useState([]);
  const [undo, setUndo] = useState(null);

  const loadTrash = useCallback(async () => {
    try {
      setTrash(await api.dashboardTrash());
    } catch (err) {
      setError(err);
    }
  }, []);

  useEffect(() => {
    loadTrash();
  }, [loadTrash]);

  useEffect(() => {
    if (!undo) return undefined;
    const timer = setTimeout(() => setUndo(null), UNDO_SECONDS * 1000);
    return () => clearTimeout(timer);
  }, [undo]);

  const [version, setVersion] = useState(0);
  const [period, setPeriod] = useState(null);

  useEffect(() => {
    setPeriod(null);
  }, [openId]);

  useEffect(() => {
    if (!openId) {
      setCurrent(null);
      return;
    }
    let alive = true;
    setLoading(true);
    api
      .dashboard(openId, true, period)
      .then((d) => {
        if (!alive) return;
        setCurrent(d);
        setError(null);
      })
      .catch((err) => alive && setError(err))
      .finally(() => alive && setLoading(false));
    return () => {
      alive = false;
    };
  }, [openId, version, period]);

  const refresh = useCallback(() => setVersion((v) => v + 1), []);

  const removeWidget = useCallback(
    async (dashboardId, widgetId, title) => {
      await api.removeWidget(dashboardId, widgetId);
      setCurrent((d) => (d && d.id === dashboardId ? { ...d, widgets: d.widgets.filter((w) => w.id !== widgetId) } : d));
      setUndo({ kind: "widget", id: widgetId, dashboard_id: dashboardId, title });
      await Promise.all([reload(), loadTrash()]);
    },
    [reload, loadTrash],
  );

  const restore = useCallback(
    async (item) => {
      try {
        if (item.kind === "widget") await api.restoreWidget(item.dashboard_id, item.id);
        else await api.restoreDashboard(item.id);
        setUndo(null);
        setError(null);
        refresh();
        await Promise.all([reload(), loadTrash()]);
        return true;
      } catch (err) {
        setUndo(null);
        setError(err);
        await loadTrash();
        return false;
      }
    },
    [refresh, reload, loadTrash],
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
    async (id, title) => {
      await api.deleteDashboard(id);
      setUndo({ kind: "dashboard", id, dashboard_id: id, title });
      await Promise.all([reload(), loadTrash()]);
    },
    [reload, loadTrash],
  );

  const [exporting, setExporting] = useState(null);

  const exportFile = useCallback(
    async (format, widgetId = null) => {
      if (!current) return;
      setExporting(widgetId ? `${format}:${widgetId}` : format);
      try {
        const { blob, filename } = await api.exportDashboard(current.id, { format, period, widgetId });
        saveBlob(blob, filename || `dashboard.${format}`);
        setError(null);
      } catch (err) {
        setError(err);
      } finally {
        setExporting(null);
      }
    },
    [current, period],
  );

  const exportImage = useCallback(async (node, title, key = "png") => {
    if (!node) return;
    setExporting(key);
    try {
      saveBlob(await snapshotPng(node), imageFileName(title));
      setError(null);
    } catch {
      setError(new Error("Не удалось собрать картинку в этом браузере. Сохраните дашборд кнопкой PDF."));
    } finally {
      setExporting(null);
    }
  }, []);

  return {
    items, current, error, loading, period, setPeriod, reload, refresh, remove, removeWidget, rename, exporting, exportFile, exportImage,
    trash, loadTrash, undo, setUndo, restore,
  };
}
