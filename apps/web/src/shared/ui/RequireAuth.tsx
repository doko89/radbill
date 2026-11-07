import type { ReactNode } from "react";
import { Navigate } from "react-router-dom";
import { useAuth } from "../../features/auth/AuthProvider";

export function RequireAuth({ children }: { children: ReactNode }) {
  const auth = useAuth();
  if (auth.loading) {
    return <p className="muted">Loading…</p>;
  }
  if (!auth.user) {
    return <Navigate to="/login" replace />;
  }
  return <>{children}</>;
}
