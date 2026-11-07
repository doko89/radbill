import { z } from "zod";
import { billingRequest, withQuery } from "../../shared/lib/billing-api";

/**
 * Laporan admin (agen G3, kontrak D2 didikte, JSON; ekspor CSV = G4).
 * Filter tanggal `YYYY-MM-DD` (dari input date). Semua envelope `{data}`.
 */

// --- pendapatan ---

export const revenuePointSchema = z.object({
  period: z.string(),
  total: z.number(),
});
export type RevenuePoint = z.infer<typeof revenuePointSchema>;

export const revenueReportSchema = z.object({
  points: z.array(revenuePointSchema),
  total: z.number(),
});
export type RevenueReport = z.infer<typeof revenueReportSchema>;

const revenueEnvelope = z.object({ data: revenueReportSchema });

export type RevenueGroupBy = "day" | "month";

export async function getRevenueReport(input: {
  from?: string | undefined;
  to?: string | undefined;
  group_by: RevenueGroupBy;
}): Promise<RevenueReport> {
  const raw: unknown = await billingRequest(
    withQuery("/api/v1/admin/reports/revenue", {
      from: input.from ?? null,
      to: input.to ?? null,
      group_by: input.group_by,
    }),
  );
  return revenueEnvelope.parse(raw).data;
}

// --- pemakaian ---

export const reportUsagePointSchema = z.object({
  date: z.string(),
  bytes_in: z.number(),
  bytes_out: z.number(),
});
export type ReportUsagePoint = z.infer<typeof reportUsagePointSchema>;

export const reportUsageTotalsSchema = z.object({
  bytes_in: z.number(),
  bytes_out: z.number(),
});
export type ReportUsageTotals = z.infer<typeof reportUsageTotalsSchema>;

export const usageReportSchema = z.object({
  points: z.array(reportUsagePointSchema),
  totals: reportUsageTotalsSchema,
});
export type UsageReport = z.infer<typeof usageReportSchema>;

const usageEnvelope = z.object({ data: usageReportSchema });

export async function getUsageReport(input: {
  from?: string | undefined;
  to?: string | undefined;
}): Promise<UsageReport> {
  const raw: unknown = await billingRequest(
    withQuery("/api/v1/admin/reports/usage", {
      from: input.from ?? null,
      to: input.to ?? null,
    }),
  );
  return usageEnvelope.parse(raw).data;
}

// --- piutang ---

export const receivableInvoiceSchema = z.object({
  id: z.string(),
  number: z.string(),
  customer_name: z.string(),
  total: z.number(),
  due_date: z.string(),
  days_overdue: z.number(),
});
export type ReceivableInvoice = z.infer<typeof receivableInvoiceSchema>;

export const receivablesReportSchema = z.object({
  invoices: z.array(receivableInvoiceSchema),
  total: z.number(),
});
export type ReceivablesReport = z.infer<typeof receivablesReportSchema>;

const receivablesEnvelope = z.object({ data: receivablesReportSchema });

export async function getReceivablesReport(): Promise<ReceivablesReport> {
  const raw: unknown = await billingRequest("/api/v1/admin/reports/receivables");
  return receivablesEnvelope.parse(raw).data;
}

// --- pertumbuhan ---

export const growthPointSchema = z.object({
  period: z.string(),
  new: z.number(),
  churn: z.number(),
  active: z.number(),
});
export type GrowthPoint = z.infer<typeof growthPointSchema>;

export const growthReportSchema = z.object({
  points: z.array(growthPointSchema),
});
export type GrowthReport = z.infer<typeof growthReportSchema>;

const growthEnvelope = z.object({ data: growthReportSchema });

export async function getGrowthReport(months: number): Promise<GrowthReport> {
  const raw: unknown = await billingRequest(
    withQuery("/api/v1/admin/reports/growth", { months }),
  );
  return growthEnvelope.parse(raw).data;
}
