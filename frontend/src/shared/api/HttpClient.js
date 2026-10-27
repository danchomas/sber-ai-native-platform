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

  async stream(path, body, onEvent, { signal } = {}) {
    const resp = await fetch(this.baseUrl + path, {
      method: "POST",
      headers: { "Content-Type": "application/json", Accept: "text/event-stream" },
      body: JSON.stringify(body),
      signal,
    });
    if (!resp.ok || !resp.body) {
      const text = await resp.text();
      let e = null;
      try {
        e = JSON.parse(text)?.error;
      } catch {
        e = null;
      }
      throw new ApiError(e?.message || `Не удалось выполнить запрос (${resp.status}).`, e?.code, e?.field);
    }

    const reader = resp.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    let result = null;
    for (;;) {
      const { value, done } = await reader.read();
      buffer += decoder.decode(value || new Uint8Array(), { stream: !done });
      let cut;
      while ((cut = buffer.indexOf("\n\n")) >= 0) {
        const event = parseEvent(buffer.slice(0, cut));
        buffer = buffer.slice(cut + 2);
        if (!event) continue;
        if (event.name === "error") throw new ApiError(event.data?.message || "Не удалось получить ответ.", event.data?.code);
        if (event.name === "answer") result = event.data;
        onEvent?.(event.name, event.data);
      }
      if (done) break;
    }
    if (!result) throw new ApiError("Ответ прервался. Попробуйте ещё раз.", "stream_closed");
    return result;
  }

  get(path, opts) { return this.request("GET", path, opts); }
  post(path, body, opts) { return this.request("POST", path, { ...opts, body }); }
  patch(path, body, opts) { return this.request("PATCH", path, { ...opts, body }); }
  del(path, opts) { return this.request("DELETE", path, opts); }
  upload(path, form, opts) { return this.request("POST", path, { ...opts, form }); }
}

function parseEvent(block) {
  let name = "message";
  const data = [];
  for (const line of block.split("\n")) {
    if (line.startsWith("event:")) name = line.slice(6).trim();
    else if (line.startsWith("data:")) data.push(line.slice(5).trimStart());
  }
  if (!data.length) return null;
  try {
    return { name, data: JSON.parse(data.join("\n")) };
  } catch {
    return null;
  }
}

export { parseEvent };
