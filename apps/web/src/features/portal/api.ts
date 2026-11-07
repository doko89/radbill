import { z } from "zod";
import { ApiError } from "../../shared/lib/api";
import { billingRequest, withQuery } from "../../shared/lib/billing-api";
import { firstNumber, firstText } from "../../shared/lib/billing-format";

/**
 * Portal pelanggan (agen C3, G2): kontrak API + skema zod + fetcher.
 *
 * Modul bersama namespace `portal-*`: sub-fitur (portal-dashboard,
 * portal-invoices, portal-usage, portal-profile) HANYA mengimpor dari sini,
 * tidak pernah satu sama lain. Semua field snake_case, envelope `{data}`.
 *
 * Kontrak final milik C3 (backend `portalpres`, sudah diimplementasi):
 * - GET /api/v1/portal/summary → {data: PortalSummary}
 * - GET /api/v1/portal/invoices?status= → {data: PortalInvoice[], total}
 * - GET /api/v1/portal/invoices/:id → {data: PortalInvoiceDetail}
 * - GET /api/v1/portal/usage?days= → {data: PortalUsage}
 *
 * Kontrak G3/D2 (agen G3, top-up + ganti paket, didikte):
 * - POST /api/v1/portal/topup {pppoe_account_id, bytes} → {data:{invoice}}
 * - POST /api/v1/portal/plan-changes {pppoe_account_id, to_package_id}
 * - GET /api/v1/portal/plan-changes → {data:[...]}
 * - POST /api/v1/portal/plan-changes/:id/cancel → {data:{...}}
 * - Summary: tiap akun membawa `remaining` opsional (field lama utuh).
 * - ASUMSI: GET /api/v1/portal/packages → {data:[...],total} (D2
 *   menambahkan; bila 404, UI paket menampilkan fallback hubungi admin).
 *
 * ASUMSI kontrak milik C1/C2 (belum final saat G2 ditulis; samakan saat
 * integrasi, lihat laporan agen):
 * - POST /api/v1/portal/invoices/:id/pay → {data: PayAttempt}
 *   didikte: {qr_payload, expires_at, amount, unique_code, message}.
 *   Asumsi C3: respons JUGA membawa `attempt_id` (fallback: `id`) agar
 *   portal bisa polling status. Bila C1/C2 menamakannya lain, ubah
 *   payAttemptId() + skema di bawah, tidak perlu menyentuh komponen.
 * - GET /api/v1/portal/payment-attempts/:id → {data: AttemptStatus}
 *   Asumsi C3: {id, invoice_id?, status, amount?, qr_payload?, expires_at?,
 *   paid_at?}; status ∈ pending|paid|expired|cancelled|failed (kolom DB
 *   payment_attempts.status).
 * - POST /api/v1/auth/password/change milik C1: body
 *   {current_password, new_password}; respons 2xx apa pun dianggap sukses.
 * - Error envelope kode bisnis: {error:{code:"gobiz_offline"|
 *   "quota_exceeded", message}} → sembunyikan tombol Bayar (PRD §6.4).
 */

// --- summary (final C3) ---

export const portalCustomerSchema = z.object({
  id: z.string(),
  name: z.string(),
  phone: z.string().nullable(),
});
export type PortalCustomer = z.infer<typeof portalCustomerSchema>;

export const portalPackageSchema = z.object({
  id: z.string(),
  name: z.string(),
  price: z.number(),
  rate_limit: z.string().nullable(),
  quota_bytes: z.number().nullable(),
  online_seconds_limit: z.number().nullable(),
  validity_seconds: z.number().nullable(),
  // Opsional (G3): bila D2 menambahkannya ke summary, dipakai untuk
  // menentukan upgrade_group paket aktif. Absen = undefined, aman.
  allow_self_service: z.boolean().nullish(),
  upgrade_group: z.string().nullish(),
});
export type PortalPackage = z.infer<typeof portalPackageSchema>;

/** Sisa jatah per akun (G3, ADITIF: field lama summary utuh). */
export const portalRemainingSchema = z.object({
  quota_bytes: z.number().nullable(),
  quota_used: z.number(),
  online_seconds: z.number().nullable(),
  online_used: z.number(),
});
export type PortalRemaining = z.infer<typeof portalRemainingSchema>;

export const portalAccountSchema = z.object({
  id: z.string(),
  username: z.string(),
  status: z.string(),
  expires_at: z.string().nullable(),
  package: portalPackageSchema,
  // `.nullish()` agar ringkasan backend lama (tanpa `remaining`) tetap lolos parse.
  remaining: portalRemainingSchema.nullish(),
});
export type PortalAccount = z.infer<typeof portalAccountSchema>;

export const portalQuotaSchema = z.object({
  limit_bytes: z.number().nullable(),
  used_bytes: z.number(),
  remaining_bytes: z.number().nullable(),
});
export type PortalQuota = z.infer<typeof portalQuotaSchema>;

export const portalOnlineTimeSchema = z.object({
  limit_seconds: z.number().nullable(),
  used_seconds: z.number(),
  remaining_seconds: z.number().nullable(),
});
export type PortalOnlineTime = z.infer<typeof portalOnlineTimeSchema>;

export const portalDueSchema = z.object({
  invoice_id: z.string(),
  number: z.string(),
  total: z.number(),
  due_date: z.string(),
});
export type PortalDue = z.infer<typeof portalDueSchema>;

export const portalSummarySchema = z.object({
  customer: portalCustomerSchema.nullable(),
  accounts: z.array(portalAccountSchema),
  default_account_id: z.string().nullable(),
  quota: portalQuotaSchema,
  online_time: portalOnlineTimeSchema,
  due: portalDueSchema.nullable(),
  unpaid_count: z.number(),
  unpaid_total: z.number(),
});
export type PortalSummary = z.infer<typeof portalSummarySchema>;

const summaryEnvelope = z.object({ data: portalSummarySchema });

export async function getPortalSummary(): Promise<PortalSummary> {
  const raw: unknown = await billingRequest("/api/v1/portal/summary");
  return summaryEnvelope.parse(raw).data;
}

// --- invoices (final C3) ---

export const portalPendingAttemptSchema = z.object({
  id: z.string(),
  amount: z.number(),
  qr_payload: z.string(),
  expires_at: z.string(),
});
export type PortalPendingAttempt = z.infer<typeof portalPendingAttemptSchema>;

export const portalInvoiceSchema = z.object({
  id: z.string(),
  number: z.string(),
  pppoe_account_id: z.string(),
  pppoe_username: z.string(),
  package_name: z.string(),
  total: z.number(),
  due_date: z.string(),
  status: z.string(),
  issued_at: z.string(),
  paid_at: z.string().nullable(),
  pending_attempt: portalPendingAttemptSchema.nullable(),
});
export type PortalInvoice = z.infer<typeof portalInvoiceSchema>;

export const portalInvoiceItemSchema = z.object({
  id: z.string(),
  description: z.string(),
  qty: z.number(),
  price: z.number(),
});
export type PortalInvoiceItem = z.infer<typeof portalInvoiceItemSchema>;

export const portalPaymentSchema = z.object({
  id: z.string(),
  amount: z.number(),
  method: z.string(),
  attempt_id: z.string().nullable(),
  paid_at: z.string(),
});
export type PortalPayment = z.infer<typeof portalPaymentSchema>;

export const portalInvoiceDetailSchema = portalInvoiceSchema.extend({
  items: z.array(portalInvoiceItemSchema),
  payments: z.array(portalPaymentSchema),
});
export type PortalInvoiceDetail = z.infer<typeof portalInvoiceDetailSchema>;

const invoiceListEnvelope = z.object({ data: z.array(portalInvoiceSchema), total: z.number() });
const invoiceDetailEnvelope = z.object({ data: portalInvoiceDetailSchema });

export type PortalInvoiceList = {
  data: PortalInvoice[];
  total: number;
};

export async function listPortalInvoices(status?: string): Promise<PortalInvoiceList> {
  const raw: unknown = await billingRequest(
    withQuery("/api/v1/portal/invoices", { status: status ?? null }),
  );
  return invoiceListEnvelope.parse(raw);
}

export async function getPortalInvoice(id: string): Promise<PortalInvoiceDetail> {
  const raw: unknown = await billingRequest(`/api/v1/portal/invoices/${id}`);
  return invoiceDetailEnvelope.parse(raw).data;
}

// --- usage (final C3) ---

export const portalUsagePointSchema = z.object({
  date: z.string(),
  bytes_in: z.number(),
  bytes_out: z.number(),
  online_seconds: z.number(),
});
export type PortalUsagePoint = z.infer<typeof portalUsagePointSchema>;

export const portalUsageTotalsSchema = z.object({
  bytes_in: z.number(),
  bytes_out: z.number(),
  online_seconds: z.number(),
});
export type PortalUsageTotals = z.infer<typeof portalUsageTotalsSchema>;

export const portalUsageSchema = z.object({
  days: z.number(),
  points: z.array(portalUsagePointSchema),
  totals: portalUsageTotalsSchema,
});
export type PortalUsage = z.infer<typeof portalUsageSchema>;

const usageEnvelope = z.object({ data: portalUsageSchema });

export async function getPortalUsage(days: number): Promise<PortalUsage> {
  const raw: unknown = await billingRequest(withQuery("/api/v1/portal/usage", { days }));
  return usageEnvelope.parse(raw).data;
}

// --- pay + attempt status (ASUMSI C1/C2, lihat header) ---

export const payAttemptSchema = z.object({
  attempt_id: z.string().nullish(),
  id: z.string().nullish(),
  qr_payload: z.string(),
  expires_at: z.string(),
  amount: z.number(),
  unique_code: z.number(),
  message: z.string().nullish(),
});
export type PayAttempt = z.infer<typeof payAttemptSchema>;

const payEnvelope = z.object({ data: payAttemptSchema });

export async function payPortalInvoice(invoiceId: string): Promise<PayAttempt> {
  const raw: unknown = await billingRequest(`/api/v1/portal/invoices/${invoiceId}/pay`, {
    method: "POST",
    body: {},
  });
  return payEnvelope.parse(raw).data;
}

/** Attempt id untuk polling (asumsi: `attempt_id`, fallback `id`). */
export function payAttemptId(attempt: PayAttempt): string | null {
  if (attempt.attempt_id !== null && attempt.attempt_id !== undefined) {
    return attempt.attempt_id;
  }
  return attempt.id ?? null;
}

export const attemptStatusSchema = z.object({
  id: z.string(),
  invoice_id: z.string().nullish(),
  status: z.string(),
  amount: z.number().nullish(),
  qr_payload: z.string().nullish(),
  expires_at: z.string().nullish(),
  paid_at: z.string().nullish(),
});
export type AttemptStatus = z.infer<typeof attemptStatusSchema>;

const attemptEnvelope = z.object({ data: attemptStatusSchema });

export async function getPaymentAttempt(attemptId: string): Promise<AttemptStatus> {
  const raw: unknown = await billingRequest(`/api/v1/portal/payment-attempts/${attemptId}`);
  return attemptEnvelope.parse(raw).data;
}

// --- kode error kanal bayar (PRD §6.4) ---

/** Kanal QRIS mati: sembunyikan tombol Bayar, tawarkan bayar manual. */
export const PAY_CHANNEL_DOWN_CODES: readonly string[] = ["gobiz_offline", "quota_exceeded"];

export function isPayChannelDown(err: unknown): boolean {
  if (!(err instanceof ApiError)) {
    return false;
  }
  return err.code !== undefined && PAY_CHANNEL_DOWN_CODES.includes(err.code);
}

// --- top-up kuota (G3, kontrak D2 didikte) ---
//
// POST /api/v1/portal/topup {pppoe_account_id, bytes} → 201
// {data:{invoice:{id,number,total}}}; bytes ∈ {1 GB, 5 GB, 10 GB}.

export const TOPUP_PRESETS_BYTES = [1073741824, 5368709120, 10737418240] as const;
export type TopupPresetBytes = (typeof TOPUP_PRESETS_BYTES)[number];

export const topupInvoiceSchema = z.object({
  id: z.string(),
  number: z.string(),
  total: z.number(),
});
export type TopupInvoice = z.infer<typeof topupInvoiceSchema>;

const topupEnvelope = z.object({ data: z.object({ invoice: topupInvoiceSchema }) });

export async function createPortalTopup(input: {
  pppoe_account_id: string;
  bytes: number;
}): Promise<TopupInvoice> {
  const raw: unknown = await billingRequest("/api/v1/portal/topup", {
    method: "POST",
    body: { pppoe_account_id: input.pppoe_account_id, bytes: input.bytes },
  });
  return topupEnvelope.parse(raw).data.invoice;
}

// --- ganti paket mandiri (G3, kontrak D2 didikte) ---
//
// POST /api/v1/portal/plan-changes {pppoe_account_id,to_package_id}
//   → {data:{status,effective_at,prorata_invoice_id?}} (409 bila tunggakan/tak layak)
// GET /api/v1/portal/plan-changes → {data:[...]}
// POST /api/v1/portal/plan-changes/:id/cancel → {data:{...}}
//
// Bentuk item riwayat tidak didikte penuh: skema di bawah hanya mewajibkan
// `id` + `status`, sisanya opsional + toleran alias (from_package vs
// from_package_id/name, dst.) agar tidak pecah saat field D2 sedikit beda.

export const planChangeSchema = z.object({
  id: z.string(),
  pppoe_account_id: z.string().nullish(),
  pppoe_username: z.string().nullish(),
  from_package: z.string().nullish(),
  from_package_id: z.string().nullish(),
  from_package_name: z.string().nullish(),
  to_package: z.string().nullish(),
  to_package_id: z.string().nullish(),
  to_package_name: z.string().nullish(),
  status: z.string(),
  effective_at: z.string().nullish(),
  prorata_invoice_id: z.string().nullish(),
  created_at: z.string().nullish(),
});
export type PortalPlanChange = z.infer<typeof planChangeSchema>;

export function planChangeFromLabel(change: PortalPlanChange): string {
  return firstText(
    change.from_package_name,
    change.from_package,
    change.from_package_id,
    change.pppoe_username,
  );
}

export function planChangeToLabel(change: PortalPlanChange): string {
  return firstText(change.to_package_name, change.to_package, change.to_package_id);
}

const planChangeListEnvelope = z.object({ data: z.array(planChangeSchema) });

export async function listPortalPlanChanges(): Promise<PortalPlanChange[]> {
  const raw: unknown = await billingRequest("/api/v1/portal/plan-changes");
  return planChangeListEnvelope.parse(raw).data;
}

export const planChangeResultSchema = z.object({
  id: z.string().nullish(),
  status: z.string(),
  effective_at: z.string().nullish(),
  prorata_invoice_id: z.string().nullish(),
});
export type PortalPlanChangeResult = z.infer<typeof planChangeResultSchema>;

const planChangeResultEnvelope = z.object({ data: planChangeResultSchema });

export async function createPortalPlanChange(input: {
  pppoe_account_id: string;
  to_package_id: string;
}): Promise<PortalPlanChangeResult> {
  const raw: unknown = await billingRequest("/api/v1/portal/plan-changes", {
    method: "POST",
    body: { pppoe_account_id: input.pppoe_account_id, to_package_id: input.to_package_id },
  });
  return planChangeResultEnvelope.parse(raw).data;
}

const planChangeCancelEnvelope = z.object({ data: planChangeResultSchema.partial() });
export type PortalPlanChangeCancelResult = z.infer<typeof planChangeCancelEnvelope>["data"];

export async function cancelPortalPlanChange(id: string): Promise<PortalPlanChangeCancelResult> {
  const raw: unknown = await billingRequest(`/api/v1/portal/plan-changes/${id}/cancel`, {
    method: "POST",
    body: {},
  });
  return planChangeCancelEnvelope.parse(raw).data;
}

// --- daftar paket publik untuk self-service (G3, ASUMSI endpoint D2) ---
//
// GET /api/v1/portal/packages → {data:[...],total}. Endpoint ini BELUM ADA
// saat G3 ditulis; D2 menambahkan endpoint kecil ini. Bila 404 saat
// integrasi, halaman paket menampilkan fallback "hubungi admin" (tidak crash).

export const portalPublicPackageSchema = z.object({
  id: z.string(),
  name: z.string().nullish(),
  nama: z.string().nullish(),
  price: z.number().nullish(),
  harga: z.number().nullish(),
  rate_limit: z.string().nullish(),
  quota_bytes: z.number().nullish(),
  online_seconds_limit: z.number().nullish(),
  validity_seconds: z.number().nullish(),
  allow_self_service: z.boolean().nullish(),
  upgrade_group: z.string().nullish(),
});
export type PortalPublicPackage = z.infer<typeof portalPublicPackageSchema>;

export function portalPublicPackageName(pkg: PortalPublicPackage): string {
  return firstText(pkg.nama, pkg.name, pkg.id);
}

export function portalPublicPackagePrice(pkg: PortalPublicPackage): number | null {
  return firstNumber(pkg.harga, pkg.price);
}

const portalPackageListEnvelope = z.object({
  data: z.array(portalPublicPackageSchema),
  total: z.number(),
});

export type PortalPackageList = {
  data: PortalPublicPackage[];
  total: number;
};

export async function listPortalPackages(): Promise<PortalPackageList> {
  const raw: unknown = await billingRequest("/api/v1/portal/packages");
  return portalPackageListEnvelope.parse(raw);
}

// --- ganti password (ASUMSI endpoint C1) ---

export const portalChangePasswordSchema = z
  .object({
    current_password: z.string().min(1, "kata sandi lama wajib diisi").max(256),
    new_password: z.string().min(8, "kata sandi baru minimal 8 karakter").max(256),
    confirm_password: z.string().min(1, "konfirmasi wajib diisi").max(256),
  })
  .refine((value) => value.new_password === value.confirm_password, {
    message: "konfirmasi tidak sama",
    path: ["confirm_password"],
  });
export type PortalChangePasswordInput = z.infer<typeof portalChangePasswordSchema>;

export async function changePortalPassword(input: {
  current_password: string;
  new_password: string;
}): Promise<void> {
  await billingRequest<unknown>("/api/v1/auth/password/change", {
    method: "POST",
    body: { current_password: input.current_password, new_password: input.new_password },
  });
}
