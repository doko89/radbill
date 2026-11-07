import type { ReactNode } from "react";
import { Navigate, useLocation } from "react-router-dom";
import { useBillingAuth } from "../../billing-auth/BillingAuthProvider";

/**
 * Guard portal: butuh sesi billing; role `user` → portal, role `admin` →
 * tetap admin (PRD §3: admin memegang /admin/*, user hanya /portal/*).
 */
export function RequirePortal({ children }: { children: ReactNode }) {
  const auth = useBillingAuth();
  const location = useLocation();

  if (auth.loading) {
    return <p className="muted">Memuat…</p>;
  }
  if (!auth.user) {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />;
  }
  if (auth.user.role === "admin") {
    return <Navigate to="/admin" replace />;
  }
  return <>{children}</>;
}
