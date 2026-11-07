import { z } from "zod";
import { billingRequest, billingUpdate, withQuery } from "../../shared/lib/billing-api";
import { firstText } from "../../shared/lib/billing-format";

export const pppoeAccountSchema = z.object({
  id: z.string(),
  customer_id: z.string().nullish(),
  customer_name: z.string().nullish(),
  customer_nama: z.string().nullish(),
  package_id: z.string().nullish(),
  package_name: z.string().nullish(),
  package_nama: z.string().nullish(),
  nas_id: z.string().nullish(),
  username: z.string().nullish(),
  password: z.string().nullish(),
  status: z.string().nullish(),
  expires_at: z.string().nullish(),
  static_ip: z.string().nullish(),
  created_at: z.string().nullish(),
  updated_at: z.string().nullish(),
});
export type PppoeAccount = z.infer<typeof pppoeAccountSchema>;

const pppoeListEnvelope = z.object({ data: z.array(pppoeAccountSchema), total: z.number() });
const pppoeItemEnvelope = z.object({ data: pppoeAccountSchema });

export type PppoeListParams = {
  search?: string | undefined;
  status?: string | undefined;
  customer_id?: string | undefined;
  page?: number | undefined;
  per_page?: number | undefined;
};

export type PppoeList = {
  data: PppoeAccount[];
  total: number;
};

export async function listPppoeAccounts(params: PppoeListParams = {}): Promise<PppoeList> {
  const raw: unknown = await billingRequest(
    withQuery("/api/v1/admin/pppoe-accounts", { ...params, q: params.search ?? null }),
  );
  return pppoeListEnvelope.parse(raw);
}

/** Detail akun; `reveal=true` meminta password plaintext (aksi eksplisit, diaudit server). */
export async function getPppoeAccount(id: string, reveal: boolean): Promise<PppoeAccount> {
  const raw: unknown = await billingRequest(
    withQuery(`/api/v1/admin/pppoe-accounts/${id}`, { reveal: reveal ? true : null }),
  );
  return pppoeItemEnvelope.parse(raw).data;
}

export type PppoeFormInput = {
  customer_id: string;
  package_id: string;
  nas_id?: string | undefined;
  username: string;
  password?: string | undefined;
  status?: string | undefined;
  expires_at?: string | undefined;
  static_ip?: string | undefined;
};

function pppoeBody(input: PppoeFormInput): Record<string, string> {
  const body: Record<string, string> = {
    customer_id: input.customer_id,
    package_id: input.package_id,
    username: input.username,
  };
  if (input.nas_id) {
    body["nas_id"] = input.nas_id;
  }
  if (input.password) {
    body["password"] = input.password;
  }
  if (input.status) {
    body["status"] = input.status;
  }
  if (input.expires_at) {
    body["expires_at"] = input.expires_at;
  }
  if (input.static_ip) {
    body["static_ip"] = input.static_ip;
  }
  return body;
}

export async function createPppoeAccount(input: PppoeFormInput): Promise<PppoeAccount> {
  const raw: unknown = await billingRequest("/api/v1/admin/pppoe-accounts", {
    method: "POST",
    body: pppoeBody(input),
  });
  return pppoeItemEnvelope.parse(raw).data;
}

export async function updatePppoeAccount(id: string, input: PppoeFormInput): Promise<PppoeAccount> {
  const raw: unknown = await billingUpdate(`/api/v1/admin/pppoe-accounts/${id}`, pppoeBody(input));
  return pppoeItemEnvelope.parse(raw).data;
}

export async function deletePppoeAccount(id: string): Promise<void> {
  await billingRequest<unknown>(`/api/v1/admin/pppoe-accounts/${id}`, { method: "DELETE" });
}

export type PppoeAction = "isolate" | "activate" | "disable" | "disconnect" | "reset-quota";

export async function pppoeAccountAction(id: string, action: PppoeAction): Promise<void> {
  await billingRequest<unknown>(`/api/v1/admin/pppoe-accounts/${id}/${action}`, {
    method: "POST",
    body: {},
  });
}

export async function extendPppoeAccount(id: string, days: number): Promise<void> {
  await billingRequest<unknown>(`/api/v1/admin/pppoe-accounts/${id}/extend`, {
    method: "POST",
    body: { days, validity_seconds: days * 86400 },
  });
}

export async function changePppoePackage(id: string, packageId: string): Promise<void> {
  await billingRequest<unknown>(`/api/v1/admin/pppoe-accounts/${id}/change-package`, {
    method: "POST",
    body: { package_id: packageId, new_package_id: packageId },
  });
}

export type PortalCredential = {
  username: string | null;
  password: string | null;
};

function extractCredential(raw: unknown): PortalCredential {
  if (typeof raw !== "object" || raw === null) {
    return { username: null, password: null };
  }
  const data: unknown = (raw as { data?: unknown }).data ?? raw;
  if (typeof data !== "object" || data === null) {
    return { username: null, password: null };
  }
  const record = data as Record<string, unknown>;
  const pick = (...keys: string[]): string | null => {
    for (const key of keys) {
      const value: unknown = record[key];
      if (typeof value === "string" && value.length > 0) {
        return value;
      }
    }
    return null;
  };
  return {
    username: pick("username", "portal_username", "login"),
    password: pick("password", "portal_password", "new_password", "temporary_password"),
  };
}

export type PortalAccountInput = {
  username?: string | undefined;
  password?: string | undefined;
};

function portalBody(input: PortalAccountInput): Record<string, string> {
  const body: Record<string, string> = {};
  if (input.username !== undefined && input.username !== "") {
    body["username"] = input.username;
  }
  if (input.password !== undefined && input.password !== "") {
    body["password"] = input.password;
  }
  return body;
}

export async function createPortalAccount(
  id: string,
  input: PortalAccountInput,
): Promise<PortalCredential> {
  const raw: unknown = await billingRequest(`/api/v1/admin/pppoe-accounts/${id}/portal-account`, {
    method: "POST",
    body: portalBody(input),
  });
  return extractCredential(raw);
}

export async function resetPortalAccount(
  id: string,
  input: PortalAccountInput,
): Promise<PortalCredential> {
  const raw: unknown = await billingRequest(
    `/api/v1/admin/pppoe-accounts/${id}/portal-account/reset`,
    { method: "POST", body: portalBody(input) },
  );
  return extractCredential(raw);
}

export const pppoeFormSchema = z.object({
  customer_id: z.string().min(1, "pelanggan wajib dipilih"),
  package_id: z.string().min(1, "paket wajib dipilih"),
  username: z
    .string()
    .min(1, "username wajib diisi")
    .max(64)
    .regex(/^[a-zA-Z0-9._-]+$/, "username hanya boleh huruf, angka, titik, strip, underscore"),
  static_ip: z.string().max(64).optional(),
  expires_at: z.string().max(32).optional(),
});

export function pppoeCustomerName(account: PppoeAccount): string {
  return firstText(account.customer_name, account.customer_nama, account.customer_id);
}

export function pppoePackageName(account: PppoeAccount): string {
  return firstText(account.package_name, account.package_nama, account.package_id);
}
