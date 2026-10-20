import { useCallback, useEffect, useState } from "react";

import { Sidebar } from "./layout/Sidebar.jsx";
import { useHealth } from "./useHealth.js";
import { useTheme } from "./useTheme.js";
import { ChatPage } from "../features/chat/ChatPage.jsx";
import { useSessions } from "../features/chat/hooks/useSessions.js";
import { DashboardsPage } from "../features/dashboards/DashboardsPage.jsx";
import { DatasetsPage } from "../features/datasets/DatasetsPage.jsx";
import { useDatasets } from "../features/datasets/hooks/useDatasets.js";
import { SourcesPage } from "../features/sources/SourcesPage.jsx";
import { api } from "../shared/api/PlatformApi.js";

// App связывает разделы и держит то немногое, что их переживает:
// активный диалог, каталог и выбранный дашборд. Фичи друг о друге
// не знают — они общаются через эти несколько значений.
export function App() {
  const [view, setView] = useState("chat");
  const [openDashboardId, setOpenDashboardId] = useState(null);
  const [dashboardCount, setDashboardCount] = useState(0);

  const { theme, toggle } = useTheme();
  const health = useHealth();
  const sessions = useSessions();
  const datasets = useDatasets();

  useEffect(() => {
    api.listDashboards().then((d) => setDashboardCount(d.length)).catch(() => {});
  }, []);

  // Агент может собрать дашборд в ответ на вопрос — тогда переходим к нему.
  const showDashboard = useCallback((id) => {
    setOpenDashboardId(id);
    setView("dashboards");
    api.listDashboards().then((d) => setDashboardCount(d.length)).catch(() => {});
  }, []);

  const counts = {
    chat: sessions.items.length,
    data: datasets.items.length,
    dashboards: dashboardCount,
  };

  return (
    <>
      <Sidebar
        view={view}
        onView={setView}
        counts={counts}
        sessions={sessions.items}
        activeSession={sessions.activeId}
        onSelectSession={(id) => {
          sessions.select(id);
          setView("chat");
        }}
        onNewSession={() => {
          sessions.create();
          setView("chat");
        }}
        health={health}
        onToggleTheme={toggle}
      />

      <main className="main">
        <ChatPage
          active={view === "chat"}
          theme={theme}
          sessions={sessions}
          onOpenDashboard={showDashboard}
        />
        <DatasetsPage active={view === "data"} theme={theme} datasets={datasets} />
        <SourcesPage active={view === "sources"} onCatalogChanged={datasets.reload} />
        <DashboardsPage
          active={view === "dashboards"}
          theme={theme}
          openId={openDashboardId}
          onOpen={setOpenDashboardId}
          onCount={setDashboardCount}
        />
      </main>
    </>
  );
}
