import { ApiError } from "./api";
import { getCookie } from "./cookies";

/**
 * Billing API client for `/api/v1` (additive; the legacy `api.ts` client is untouched).
 *
 * - Access token lives in module memory only, never localStorage.
 * - Cookies are always sent (httpOnly refresh cookie).
 * - Unsafe methods carry the boilerplate `X-CSRF-Token` header.
 * - 401 triggers a single-flight cookie refresh, then one retry.
 * - Backend error envelope `{error:{code,message,field?}}` maps to `ApiError`.
 */

let accessToken: string | null = null;

export function setBillingAccessToken(token: string | null): void {
  accessToken = token;
}

export function getBillingAccessToken(): string | null {
  return accessToken;
}

export type BillingRequestOptions = {
  method?: string;
  body?: unknown;
  /** Skip the 401 → refresh → retry flow (used by login/reset endpoints). */
  noAuthRetry?: boolean;
};

function isUnsafeMethod(method: string): boolean {
  return method !== "GET" && method !== "HEAD";
}

async function ensureBillingCsrf(force: boolean): Promise<string | undefined> {
  const existing = getCookie("csrf_token");
  if (existing && !force) {
    return existing;
  }
  const res = await fetch("/api/auth/csrf", { credentials: "include" });
  if (!res.ok) {
    throw new ApiError(res.status, "gagal menyiapkan token CSRF");
  }
  return getCookie("csrf_token");
}

function parseRefreshToken(payload: unknown): string | null {
  if (typeof payload !== "object" || payload === null) {
    return null;
  }
  const data: unknown = (payload as { data?: unknown }).data;
  if (typeof data !== "object" || data === null) {
    return null;
  }
  const record = data as { access_token?: unknown };
  return typeof record.access_token === "string" && record.access_token.length > 0
    ? record.access_token
    : null;
}

let refreshFlight: Promise<string | null> | null = null;

/**
 * Single-flight refresh: concurrent 401s share one cookie-based refresh call.
 * Never triggers a nested refresh (raw fetch, no retry flags).
 */
export function refreshBillingAccessToken(): Promise<string | null> {
  if (!refreshFlight) {
    refreshFlight = (async (): Promise<string | null> => {
      try {
        const headers: Record<string, string> = { "Content-Type": "application/json" };
        const csrf = await ensureBillingCsrf(false);
        if (csrf) {
          headers["X-CSRF-Token"] = csrf;
        }
        const res = await fetch("/api/v1/auth/refresh", {
          method: "POST",
          headers,
          body: "{}",
          credentials: "include",
        });
        if (!res.ok) {
          setBillingAccessToken(null);
          return null;
        }
        let payload: unknown = null;
        try {
          payload = (await res.json()) as unknown;
        } catch {
          payload = null;
        }
        const token = parseRefreshToken(payload);
        setBillingAccessToken(token);
        return token;
      } catch {
        setBillingAccessToken(null);
        return null;
      } finally {
        refreshFlight = null;
      }
    })();
  }
  return refreshFlight;
}

function toBillingApiError(status: number, payload: unknown): ApiError {
  if (typeof payload === "object" && payload !== null && "error" in payload) {
    const record = payload as { error?: unknown };
    if (typeof record.error === "object" && record.error !== null) {
      const envelope = record.error as { code?: unknown; message?: unknown; field?: unknown };
      const message =
        typeof envelope.message === "string" && envelope.message.length > 0
          ? envelope.message
          : "permintaan gagal";
      const code =
        typeof envelope.code === "string" && envelope.code.length > 0 ? envelope.code : undefined;
      let fields: Record<string, string> | undefined;
      if (typeof envelope.field === "string" && envelope.field.length > 0) {
        fields = { [envelope.field]: message };
      }
      return new ApiError(status, message, fields, code);
    }
    if (typeof record.error === "string" && record.error.length > 0) {
      return new ApiError(status, record.error);
    }
  }
  return new ApiError(status, "permintaan gagal");
}

export async function billingRequest<T>(path: string, opts: BillingRequestOptions = {}): Promise<T> {
  return billingFetch<T>(path, opts, false, false);
}

async function billingFetch<T>(
  path: string,
  opts: BillingRequestOptions,
  authRetried: boolean,
  csrfRetried: boolean,
): Promise<T> {
  const method = opts.method ?? "GET";
  const headers: Record<string, string> = {};
  let payload: string | undefined;
  if (opts.body !== undefined) {
    headers["Content-Type"] = "application/json";
    payload = JSON.stringify(opts.body);
  }
  const token = getBillingAccessToken();
  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }
  if (isUnsafeMethod(method)) {
    const csrf = await ensureBillingCsrf(false);
    if (csrf) {
      headers["X-CSRF-Token"] = csrf;
    }
  }
  const init: RequestInit = { method, headers, credentials: "include" };
  if (payload !== undefined) {
    init.body = payload;
  }
  let res: Response;
  try {
    res = await fetch(path, init);
  } catch {
    throw new ApiError(0, "tidak dapat terhubung ke server");
  }
  if (res.status === 401 && !authRetried && opts.noAuthRetry !== true) {
    const fresh = await refreshBillingAccessToken();
    if (fresh) {
      return billingFetch<T>(path, opts, true, csrfRetried);
    }
    throw new ApiError(401, "sesi berakhir, silakan masuk kembali");
  }
  if (res.status === 403 && !csrfRetried && isUnsafeMethod(method)) {
    await ensureBillingCsrf(true);
    return billingFetch<T>(path, opts, authRetried, true);
  }
  if (!res.ok) {
    let data: unknown = null;
    try {
      data = (await res.json()) as unknown;
    } catch {
      data = null;
    }
    throw toBillingApiError(res.status, data);
  }
  if (res.status === 204) {
    return undefined as T;
  }
  return (await res.json()) as T;
}

/** PATCH with PUT fallback (405) for update endpoints whose verb is not pinned yet. */
export async function billingUpdate<T>(path: string, body: unknown): Promise<T> {
  try {
    return await billingRequest<T>(path, { method: "PATCH", body });
  } catch (err) {
    if (err instanceof ApiError && err.status === 405) {
      return billingRequest<T>(path, { method: "PUT", body });
    }
    throw err;
  }
}

export function withQuery(
  path: string,
  params: Record<string, string | number | boolean | null | undefined>,
): string {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === null || value === undefined || value === "") {
      continue;
    }
    query.set(key, String(value));
  }
  const text = query.toString();
  return text.length > 0 ? `${path}?${text}` : path;
}

function csvFilenameFromDisposition(header: string | null, fallback: string): string {
  if (header) {
    const match = /filename="?([^";]+)"?/.exec(header);
    const name = match?.[1]?.trim();
    if (name) {
      return name;
    }
  }
  return fallback;
}

/**
 * Unduh CSV mentah (G4): fetch langsung dengan Bearer + cookies, satu kali
 * refresh saat 401, lalu simpan sebagai blob. Nama file mengikuti header
 * `Content-Disposition` bila ada, atau `fallbackFilename`.
 */
export async function billingDownloadCsv(path: string, fallbackFilename: string): Promise<void> {
  async function fetchCsv(authRetried: boolean): Promise<Response> {
    const headers: Record<string, string> = {};
    const token = getBillingAccessToken();
    if (token) {
      headers["Authorization"] = `Bearer ${token}`;
    }
    let res: Response;
    try {
      res = await fetch(path, { method: "GET", headers, credentials: "include" });
    } catch {
      throw new ApiError(0, "tidak dapat terhubung ke server");
    }
    if (res.status === 401 && !authRetried) {
      const fresh = await refreshBillingAccessToken();
      if (fresh) {
        return fetchCsv(true);
      }
      throw new ApiError(401, "sesi berakhir, silakan masuk kembali");
    }
    return res;
  }

  const res = await fetchCsv(false);
  if (!res.ok) {
    let data: unknown = null;
    try {
      data = (await res.json()) as unknown;
    } catch {
      data = null;
    }
    throw toBillingApiError(res.status, data);
  }
  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  try {
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = csvFilenameFromDisposition(
      res.headers.get("Content-Disposition"),
      fallbackFilename,
    );
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
  } finally {
    URL.revokeObjectURL(url);
  }
}
