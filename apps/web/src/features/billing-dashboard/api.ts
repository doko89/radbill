import { z } from "zod";
import { billingRequest, withQuery } from "../../shared/lib/billing-api";

const totalEnvelope = z.object({ data: z.array(z.unknown()), total: z.number() });

async function fetchTotal(path: string, extra?: Record<string, string>): Promise<number | null> {
  try {
    const raw: unknown = await billingRequest(withQuery(path, { per_page: 1, ...extra }));
    return totalEnvelope.parse(raw).total;
  } catch {
    return null;
  }
}

export type DashboardTotals = {
  customers: number | null;
  packages: number | null;
  pppoeAccounts: number | null;
  nas: number | null;
  onlineSessions: number | null;
  invoices: number | null;
};

export async function fetchDashboardTotals(): Promise<DashboardTotals> {
  const [customers, packages, pppoeAccounts, nas, onlineSessions, invoices] = await Promise.all([
    fetchTotal("/api/v1/admin/customers"),
    fetchTotal("/api/v1/admin/packages"),
    fetchTotal("/api/v1/admin/pppoe-accounts"),
    fetchTotal("/api/v1/admin/nas"),
    fetchTotal("/api/v1/admin/sessions", { online: "true" }),
    fetchTotal("/api/v1/admin/invoices"),
  ]);
  return { customers, packages, pppoeAccounts, nas, onlineSessions, invoices };
}

// --- Dasbor G4 (kontrak E1 didikte, field beku) ---

export const dashboardAccountsSchema = z.object({
  active: z.number(),
  isolated: z.number(),
  disabled: z.number(),
});
export type DashboardAccounts = z.infer<typeof dashboardAccountsSchema>;

export const dashboardRecentPaymentSchema = z.object({
  id: z.string(),
  number: z.string(),
  amount: z.number(),
  paid_at: z.string(),
});
export type DashboardRecentPayment = z.infer<typeof dashboardRecentPaymentSchema>;

export const adminDashboardSchema = z.object({
  revenue_mtd: z.number(),
  outstanding_total: z.number(),
  overdue_count: z.number(),
  accounts: dashboardAccountsSchema,
  online_sessions: z.number(),
  recent_payments: z.array(dashboardRecentPaymentSchema).nullish(),
});
export type AdminDashboard = z.infer<typeof adminDashboardSchema>;

const adminDashboardEnvelope = z.object({ data: adminDashboardSchema });

export type AdminDashboardView = Omit<AdminDashboard, "recent_payments"> & {
  recent_payments: DashboardRecentPayment[];
};

export async function fetchAdminDashboard(): Promise<AdminDashboardView> {
  const raw: unknown = await billingRequest("/api/v1/admin/dashboard");
  const data = adminDashboardEnvelope.parse(raw).data;
  return { ...data, recent_payments: data.recent_payments ?? [] };
}
