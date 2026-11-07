import type { ReactNode } from "react";
import { Navigate, useLocation } from "react-router-dom";
import { useBillingAuth } from "../../features/billing-auth/BillingAuthProvider";

type LocationState = {
  from?: unknown;
};

/**
 * Billing route guard (mirrors `RequireAuth`): requires a billing session,
 * optionally requires at least one of `anyOf` permissions.
 */
export function RequireBillingPermission({
  children,
  anyOf,
}: {
  children: ReactNode;
  anyOf?: string[];
}) {
  const auth = useBillingAuth();
  const location = useLocation();

  if (auth.loading) {
    return <p className="muted">Memuat…</p>;
  }
  if (!auth.user) {
    return <Navigate to="/admin/login" replace state={{ from: location.pathname }} />;
  }
  if (
    anyOf !== undefined &&
    anyOf.length > 0 &&
    !anyOf.some((permission) => auth.hasPermission(permission))
  ) {
    return (
      <div role="alert">
        <h1>Akses ditolak</h1>
        <p className="muted">Akun Anda tidak memiliki izin untuk membuka halaman ini.</p>
      </div>
    );
  }
  return <>{children}</>;
}

export function billingLoginRedirect(state: unknown): string {
  if (typeof state === "object" && state !== null) {
    const from = (state as LocationState).from;
    if (typeof from === "string" && from.startsWith("/admin") && from !== "/admin/login") {
      return from;
    }
  }
  return "/admin";
}
