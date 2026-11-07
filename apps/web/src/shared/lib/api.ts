import { getCookie } from "./cookies";

export class ApiError extends Error {
  readonly status: number;
  readonly fields: Record<string, string> | undefined;

  constructor(status: number, message: string, fields?: Record<string, string>) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.fields = fields;
  }
}

type ApiOptions = {
  method?: string;
  body?: unknown;
};

function isSafeMethod(method: string): boolean {
  return method === "GET" || method === "HEAD";
}

async function ensureCsrf(force: boolean): Promise<string | undefined> {
  const existing = getCookie("csrf_token");
  if (existing && !force) {
    return existing;
  }
  const res = await fetch("/api/auth/csrf", { credentials: "include" });
  if (!res.ok) {
    throw new ApiError(res.status, "failed to obtain CSRF token");
  }
  return getCookie("csrf_token");
}

function toApiError(status: number, data: unknown): ApiError {
  if (typeof data === "object" && data !== null && "error" in data) {
    const record = data as { error?: unknown; fields?: unknown };
    const message = typeof record.error === "string" ? record.error : "request failed";
    let fields: Record<string, string> | undefined;
    if (typeof record.fields === "object" && record.fields !== null) {
      fields = {};
      for (const [key, value] of Object.entries(record.fields)) {
        if (typeof value === "string") {
          fields[key] = value;
        }
      }
    }
    return new ApiError(status, message, fields);
  }
  return new ApiError(status, "request failed");
}

async function parseJson(res: Response): Promise<unknown> {
  try {
    return (await res.json()) as unknown;
  } catch {
    return {};
  }
}

/** Same-origin JSON client: sends cookies, attaches the CSRF header, retries once on rotation. */
export async function api<T>(path: string, opts: ApiOptions = {}, retried = false): Promise<T> {
  const method = opts.method ?? "GET";
  const headers: Record<string, string> = {};
  let payload: string | undefined;
  if (opts.body !== undefined) {
    headers["Content-Type"] = "application/json";
    payload = JSON.stringify(opts.body);
  }
  if (!isSafeMethod(method)) {
    const token = await ensureCsrf(false);
    if (token) {
      headers["X-CSRF-Token"] = token;
    }
  }
  const init: RequestInit = { method, headers, credentials: "include" };
  if (payload !== undefined) {
    init.body = payload;
  }
  const res = await fetch(path, init);
  if (res.status === 403 && !retried && !isSafeMethod(method)) {
    await ensureCsrf(true);
    return api<T>(path, opts, true);
  }
  if (!res.ok) {
    throw toApiError(res.status, await parseJson(res));
  }
  if (res.status === 204) {
    return undefined as T;
  }
  return (await res.json()) as T;
}

/** POSTs multipart FormData with cookies + CSRF. No Content-Type: the browser sets the boundary. */
export async function apiForm<T>(path: string, form: FormData, retried = false): Promise<T> {
  const headers: Record<string, string> = {};
  const token = await ensureCsrf(false);
  if (token) {
    headers["X-CSRF-Token"] = token;
  }
  const res = await fetch(path, { method: "POST", headers, body: form, credentials: "include" });
  if (res.status === 403 && !retried) {
    await ensureCsrf(true);
    return apiForm<T>(path, form, true);
  }
  if (!res.ok) {
    throw toApiError(res.status, await parseJson(res));
  }
  return (await res.json()) as T;
}
