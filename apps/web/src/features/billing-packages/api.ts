import { z } from "zod";
import { billingRequest, billingUpdate, withQuery } from "../../shared/lib/billing-api";
import { firstNumber, firstText } from "../../shared/lib/billing-format";

/** Tipe paket = basis pembatasan utama (PRD §6.2). */
export const PACKAGE_TYPES = ["time", "quota", "fup"] as const;
export type PackageType = (typeof PACKAGE_TYPES)[number];

export const PACKAGE_TYPE_META: Record<PackageType, { title: string; desc: string }> = {
  time: { title: "Time based", desc: "Batasan jatah jam online." },
  quota: { title: "Kuota based", desc: "Batasan kuota data per periode." },
  fup: { title: "FUP based", desc: "Kecepatan penuh sampai ambang, lalu turun." },
};

export function isPackageType(value: unknown): value is PackageType {
  return value === "time" || value === "quota" || value === "fup";
}

export const packageSchema = z.object({
  id: z.string(),
  nama: z.string().nullish(),
  name: z.string().nullish(),
  harga: z.number().nullish(),
  price: z.number().nullish(),
  periode: z.string().nullish(),
  period: z.string().nullish(),
  type: z.string().nullish(),
  rate_limit: z.string().nullish(),
  address_pool: z.string().nullish(),
  validity_seconds: z.number().nullish(),
  online_seconds_limit: z.number().nullish(),
  quota_bytes: z.number().nullish(),
  quota_reset: z.string().nullish(),
  fup_threshold_bytes: z.number().nullish(),
  fup_rate_limit: z.string().nullish(),
  on_exhausted: z.string().nullish(),
  is_public: z.boolean().nullish(),
  allow_self_service: z.boolean().nullish(),
  upgrade_group: z.string().nullish(),
  created_at: z.string().nullish(),
  updated_at: z.string().nullish(),
});
export type BillingPackage = z.infer<typeof packageSchema>;

const packageListEnvelope = z.object({ data: z.array(packageSchema), total: z.number() });
const packageItemEnvelope = z.object({ data: packageSchema });

export type PackageListParams = {
  search?: string | undefined;
  page?: number | undefined;
  per_page?: number | undefined;
};

export type PackageList = {
  data: BillingPackage[];
  total: number;
};

export async function listPackages(params: PackageListParams = {}): Promise<PackageList> {
  const raw: unknown = await billingRequest(
    withQuery("/api/v1/admin/packages", { ...params, q: params.search ?? null }),
  );
  return packageListEnvelope.parse(raw);
}

export async function getPackage(id: string): Promise<BillingPackage> {
  const raw: unknown = await billingRequest(`/api/v1/admin/packages/${id}`);
  return packageItemEnvelope.parse(raw).data;
}

export type PackageFormInput = {
  nama: string;
  type: PackageType;
  harga?: number | undefined;
  periode?: string | undefined;
  rate_limit?: string | undefined;
  address_pool?: string | undefined;
  validity_seconds?: number | undefined;
  online_seconds_limit?: number | undefined;
  quota_bytes?: number | undefined;
  quota_reset?: string | undefined;
  fup_threshold_bytes?: number | undefined;
  fup_rate_limit?: string | undefined;
  on_exhausted?: string | undefined;
  is_public?: boolean | undefined;
  allow_self_service?: boolean | undefined;
  upgrade_group?: string | undefined;
};

function packageBody(input: PackageFormInput): Record<string, string | number | boolean> {
  const body: Record<string, string | number | boolean> = {
    nama: input.nama,
    name: input.nama,
    type: input.type,
  };
  if (input.harga !== undefined) {
    body["harga"] = input.harga;
    body["price"] = input.harga;
  }
  if (input.periode) {
    body["periode"] = input.periode;
    body["period"] = input.periode;
  }
  if (input.rate_limit) {
    body["rate_limit"] = input.rate_limit;
  }
  if (input.address_pool) {
    body["address_pool"] = input.address_pool;
  }
  if (input.validity_seconds !== undefined) {
    body["validity_seconds"] = input.validity_seconds;
  }
  if (input.online_seconds_limit !== undefined) {
    body["online_seconds_limit"] = input.online_seconds_limit;
  }
  if (input.quota_bytes !== undefined) {
    body["quota_bytes"] = input.quota_bytes;
  }
  if (input.quota_reset) {
    body["quota_reset"] = input.quota_reset;
  }
  if (input.fup_threshold_bytes !== undefined) {
    body["fup_threshold_bytes"] = input.fup_threshold_bytes;
  }
  if (input.fup_rate_limit) {
    body["fup_rate_limit"] = input.fup_rate_limit;
  }
  if (input.on_exhausted) {
    body["on_exhausted"] = input.on_exhausted;
  }
  if (input.is_public !== undefined) {
    body["is_public"] = input.is_public;
  }
  if (input.allow_self_service !== undefined) {
    body["allow_self_service"] = input.allow_self_service;
  }
  if (input.upgrade_group) {
    body["upgrade_group"] = input.upgrade_group;
  }
  return body;
}

export async function createPackage(input: PackageFormInput): Promise<BillingPackage> {
  const raw: unknown = await billingRequest("/api/v1/admin/packages", {
    method: "POST",
    body: packageBody(input),
  });
  return packageItemEnvelope.parse(raw).data;
}

export async function updatePackage(id: string, input: PackageFormInput): Promise<BillingPackage> {
  const raw: unknown = await billingUpdate(`/api/v1/admin/packages/${id}`, packageBody(input));
  return packageItemEnvelope.parse(raw).data;
}

export async function deletePackage(id: string): Promise<void> {
  await billingRequest<unknown>(`/api/v1/admin/packages/${id}`, { method: "DELETE" });
}

export const packageFormSchema = z.object({
  nama: z.string().min(1, "nama wajib diisi").max(120),
});

export function packageName(pkg: BillingPackage): string {
  return firstText(pkg.nama, pkg.name, pkg.id);
}

export function packagePrice(pkg: BillingPackage): number | null {
  return firstNumber(pkg.harga, pkg.price);
}

export function packagePeriod(pkg: BillingPackage): string {
  return firstText(pkg.periode, pkg.period);
}

/**
 * Tipe efektif paket. Baris lama tanpa `type` diinferensikan dari field
 * yang terisi (ada ambang FUP → fup, ada kuota → quota, sisanya time)
 * agar dialog ubah membuka tipe yang benar.
 */
export function packageTypeOf(pkg: BillingPackage | null): PackageType {
  if (pkg && isPackageType(pkg.type)) {
    return pkg.type;
  }
  if (pkg?.fup_threshold_bytes !== null && pkg?.fup_threshold_bytes !== undefined) {
    return "fup";
  }
  if (pkg?.quota_bytes !== null && pkg?.quota_bytes !== undefined) {
    return "quota";
  }
  return "time";
}

export function packageTypeLabel(pkg: BillingPackage): string {
  return PACKAGE_TYPE_META[packageTypeOf(pkg)].title;
}
