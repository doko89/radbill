/**
 * Permission strings for billing guards. Names follow the PRD §3 examples
 * (`package:write`, `pppoe:write`, `invoice:read_own`); `admin` bypasses all
 * checks (PRD §3: admin memegang seluruh `/admin/*`).
 */
export const BillingPermissions = {
  customersRead: "customer:read",
  customersWrite: "customer:write",
  packagesRead: "package:read",
  packagesWrite: "package:write",
  pppoeRead: "pppoe:read",
  pppoeWrite: "pppoe:write",
  nasRead: "nas:read",
  nasWrite: "nas:write",
  sessionsRead: "session:read",
  invoicesRead: "invoice:read",
  invoicesWrite: "invoice:write",
  reportsRead: "report:read",
  auditRead: "audit:read",
} as const;

export type BillingPermission = (typeof BillingPermissions)[keyof typeof BillingPermissions];
