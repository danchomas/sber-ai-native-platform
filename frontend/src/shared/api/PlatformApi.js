import { HttpClient } from "./HttpClient.js";

export class PlatformApi {
  constructor(http = new HttpClient()) {
    this.http = http;
  }

  health() {
    return this.http.get("/health");
  }

  listDatasets() {
    return this.http.get("/datasets").then((r) => r?.datasets ?? []);
  }

  getDataset(id) {
    return this.http.get(`/datasets/${id}`);
  }

  datasetProfile(id) {
    return this.http.get(`/datasets/${id}/profile`);
  }

  datasetPreview(id, limit = 25) {
    return this.http.get(`/datasets/${id}/preview?limit=${limit}`);
  }

  deleteDataset(id) {
    return this.http.del(`/datasets/${id}`);
  }

  uploadDataset(file, name) {
    const form = new FormData();
    form.append("file", file);
    if (name) form.append("name", name);
    return this.http.upload("/datasets", form);
  }

  relations() {
    return this.http.get("/catalog/relations").then((r) => r?.relations ?? []);
  }

  listSources() {
    return this.http.get("/sources").then((r) => r?.sources ?? []);
  }

  attachSource(name, dsn) {
    return this.http.post("/sources", { name, dsn });
  }

  sourceTables(id) {
    return this.http.get(`/sources/${id}/tables`).then((r) => r?.tables ?? []);
  }

  attachTable(id, schema, table, name) {
    return this.http.post(`/sources/${id}/tables`, { schema, table, name: name || "" });
  }

  detachSource(id) {
    return this.http.del(`/sources/${id}`);
  }

  listSessions(limit = 50) {
    return this.http.get(`/sessions?limit=${limit}`).then((r) => r?.sessions ?? []);
  }

  createSession(title = "") {
    return this.http.post("/sessions", { title, dataset_ids: [] });
  }

  deleteSession(id) {
    return this.http.del(`/sessions/${id}`);
  }

  messages(id, limit = 100) {
    return this.http.get(`/sessions/${id}/messages?limit=${limit}`).then((r) => r?.messages ?? []);
  }

  ask(sessionId, question) {
    return this.http.post(`/sessions/${sessionId}/messages`, { question });
  }

  askStream(sessionId, question, onStep) {
    return this.http.stream(`/sessions/${sessionId}/messages/stream`, { question }, (name, data) => {
      if (name === "step") onStep?.(data);
    });
  }

  listDashboards(limit = 50) {
    return this.http.get(`/dashboards?limit=${limit}`).then((r) => r?.dashboards ?? []);
  }

  dashboard(id, refresh = true, period = null) {
    const params = new URLSearchParams({ refresh: String(refresh) });
    if (period?.from) params.set("from", period.from);
    if (period?.to) params.set("to", period.to);
    return this.http.get(`/dashboards/${id}?${params}`);
  }

  deleteDashboard(id) {
    return this.http.del(`/dashboards/${id}`);
  }

  createDashboard(title, widgets, description = "") {
    return this.http.post("/dashboards", { title, description, widgets });
  }

  renameDashboard(id, title) {
    return this.http.patch(`/dashboards/${id}`, { title });
  }

  addWidget(dashboardId, widget) {
    return this.http.post(`/dashboards/${dashboardId}/widgets`, widget);
  }

  removeWidget(dashboardId, widgetId) {
    return this.http.del(`/dashboards/${dashboardId}/widgets/${widgetId}`);
  }

  runQuery(sql, maxRows) {
    return this.http.post("/query", { sql, max_rows: maxRows ?? 0 });
  }
}

export const api = new PlatformApi();
