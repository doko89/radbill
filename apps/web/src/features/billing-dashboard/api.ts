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
