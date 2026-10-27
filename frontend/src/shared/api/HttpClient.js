// Транспорт запросов к платформе.
//
// Ошибку API (единый формат {"error":{code,message,field}}) превращает
// в Error с кодом: интерфейс показывает message как есть — он написан
// для бизнес-пользователя, а не для разработчика.
export class ApiError extends Error {
  constructor(message, code, field) {
    super(message);
    this.name = "ApiError";
    this.code = code || "http_error";
    this.field = field;
  }
}

export class HttpClient {
  constructor(baseUrl = "/api/v1") {
    this.baseUrl = baseUrl;
  }

  async request(method, path, { body, form, signal } = {}) {
    const init = { method, headers: {}, signal };
    if (form) {
      init.body = form;
    } else if (body !== undefined) {
      init.headers["Content-Type"] = "application/json";
      init.body = JSON.stringify(body);
    }

    const resp = await fetch(this.baseUrl + path, init);
    if (resp.status === 204) return null;

    const text = await resp.text();
    let payload = null;
    try {
      payload = text ? JSON.parse(text) : null;
    } catch {
      payload = null;
    }

    if (!resp.ok) {
      const e = payload?.error;
      throw new ApiError(e?.message || (resp.status >= 500 ? "Сервер временно недоступен. Попробуйте ещё раз." : `Не удалось выполнить запрос (${resp.status}).`), e?.code, e?.field);
    }
    return payload;
  }

  get(path, opts) { return this.request("GET", path, opts); }
  post(path, body, opts) { return this.request("POST", path, { ...opts, body }); }
  patch(path, body, opts) { return this.request("PATCH", path, { ...opts, body }); }
  del(path, opts) { return this.request("DELETE", path, opts); }
  upload(path, form, opts) { return this.request("POST", path, { ...opts, form }); }
}
