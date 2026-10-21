// Разделы интерфейса. Порядок — порядок в боковой панели.
//
// Разделы повторяют вертикали бэкенда: каждая фича знает только себя,
// а собирает их это описание. icon — имя глифа из shared/ui/Icon.
export const VIEWS = [
  { id: "chat", icon: "chat", title: "Чат" },
  { id: "data", icon: "data", title: "Данные" },
  { id: "sources", icon: "sources", title: "Источники" },
  { id: "dashboards", icon: "dashboards", title: "Дашборды" },
];
