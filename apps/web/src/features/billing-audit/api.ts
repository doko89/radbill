import { z } from "zod";
import {
  billingDownloadCsv,
  billingRequest,
  withQuery,
} from "../../shared/lib/billing-api";

/**
 * Audit log admin (G4, kontrak E1 didikte).
 * Filter `from`/`to` menerima YYYY-MM-DD (satu hari penuh UTC) atau RFC3339.
 * Ukuran halaman JSON tetap 50 di backend; CSV dibatasi 10.000 baris.
 */

export const PAGE_SIZE = 50;

export const auditEntrySchema = z.object({
  id: z.string(),
  actor_id: z.string().nullish(),
  actor_username: z.string(),
  action: z.string(),
  target_type: z.string(),
  target_id: z.string(),
  detail: z.string(),
  ip: z.string(),
  created_at: z.string(),
});
export type AuditEntry = z.infer<typeof auditEntrySchema>;

const auditListEnvelope = z.object({
  data: z.array(auditEntrySchema),
  total: z.number(),
});

export type AuditListParams = {
  actor?: string | undefined;
  action?: string | undefined;
  from?: string | undefined;
  to?: string | undefined;
  page?: number | undefined;
};

export type AuditList = {
  data: AuditEntry[];
  total: number;
};

export async function listAuditLogs(params: AuditListParams = {}): Promise<AuditList> {
  const raw: unknown = await billingRequest(
    withQuery("/api/v1/admin/audit-logs", { ...params }),
  );
  return auditListEnvelope.parse(raw);
}

export function auditCsvUrl(params: AuditListParams = {}): string {
  const { page: _page, ...filters } = params;
  return withQuery("/api/v1/admin/audit-logs", { ...filters, format: "csv" });
}

export async function downloadAuditCsv(params: AuditListParams = {}): Promise<void> {
  await billingDownloadCsv(auditCsvUrl(params), "audit-logs.csv");
}

/** Kosakata aksi untuk saran filter (prefix didukung backend, mis. `pppoe`). */
export const AUDIT_ACTIONS: string[] = [
  "customer.created",
  "customer.updated",
  "customer.deleted",
  "package.created",
  "package.updated",
  "package.deleted",
  "pppoe.created",
  "pppoe.updated",
  "pppoe.deleted",
  "pppoe.isolated",
  "pppoe.activated",
  "pppoe.disabled",
  "pppoe.extended",
  "pppoe.package_changed",
  "pppoe.quota_reset",
  "pppoe.disconnected",
  "pppoe.portal_created",
  "pppoe.portal_reset",
  "pppoe.password_reveal",
  "nas.created",
  "nas.updated",
  "nas.deleted",
  "invoice.created",
  "invoice.voided",
  "payment.manual",
  "settings.updated",
  "auth.password_change",
  "planchange.created",
  "planchange.cancelled",
];
