import { MotionConfig } from "motion/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import { AuthProvider } from "../features/auth/AuthProvider";
import { LoginPage } from "../features/auth/pages/LoginPage";
import { OrdersPage } from "../features/order/pages/OrdersPage";
import { ProfilePage } from "../features/profile/pages/ProfilePage";
import { BillingAuthProvider } from "../features/billing-auth/BillingAuthProvider";
import { BillingLoginPage } from "../features/billing-auth/pages/BillingLoginPage";
import { DashboardPage } from "../features/billing-dashboard/pages/DashboardPage";
import { AuditPage } from "../features/billing-audit/pages/AuditPage";
import { CustomersPage } from "../features/billing-customers/pages/CustomersPage";
import { PackagesPage } from "../features/billing-packages/pages/PackagesPage";
import { PppoeAccountsPage } from "../features/billing-pppoe-accounts/pages/PppoeAccountsPage";
import { NasPage } from "../features/billing-nas/pages/NasPage";
import { SessionsPage } from "../features/billing-sessions/pages/SessionsPage";
import { InvoicesPage } from "../features/billing-invoices/pages/InvoicesPage";
import { ReportsPage } from "../features/billing-reports/pages/ReportsPage";
import { BillingPermissions } from "../shared/lib/billing-permissions";
import { Layout } from "../shared/ui/Layout";
import { RequireAuth } from "../shared/ui/RequireAuth";
import { BillingLayout } from "../shared/ui/BillingLayout";
import { RequireBillingPermission } from "../shared/ui/RequireBillingPermission";
import { PortalLayout } from "../features/portal/components/PortalLayout";
import { RequirePortal } from "../features/portal/components/RequirePortal";
import { PortalLoginPage } from "../features/portal/pages/PortalLoginPage";
import { PortalDashboardPage } from "../features/portal-dashboard/pages/DashboardPage";
import { PortalPackagesPage } from "../features/portal-packages/pages/PackagesPage";
import { PortalInvoicesPage } from "../features/portal-invoices/pages/InvoicesPage";
import { PortalInvoiceDetailPage } from "../features/portal-invoices/pages/InvoiceDetailPage";
import { PortalUsagePage } from "../features/portal-usage/pages/UsagePage";
import { PortalProfilePage } from "../features/portal-profile/pages/ProfilePage";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: false,
      staleTime: 15_000,
    },
  },
});

export function App() {
  return (
    <MotionConfig reducedMotion="user">
      <QueryClientProvider client={queryClient}>
        <AuthProvider>
          <BillingAuthProvider>
            <BrowserRouter>
              <Routes>
                <Route path="/login" element={<LoginPage />} />
                <Route path="/admin/login" element={<BillingLoginPage />} />
                <Route path="/portal/login" element={<PortalLoginPage />} />
                <Route
                  element={
                    <RequirePortal>
                      <PortalLayout />
                    </RequirePortal>
                  }
                >
                  <Route path="/portal" element={<PortalDashboardPage />} />
                  <Route path="/portal/packages" element={<PortalPackagesPage />} />
                  <Route path="/portal/invoices" element={<PortalInvoicesPage />} />
                  <Route path="/portal/invoices/:id" element={<PortalInvoiceDetailPage />} />
                  <Route path="/portal/usage" element={<PortalUsagePage />} />
                  <Route path="/portal/profile" element={<PortalProfilePage />} />
                </Route>
                <Route
                  element={
                    <RequireBillingPermission>
                      <BillingLayout />
                    </RequireBillingPermission>
                  }
                >
                  <Route path="/admin" element={<DashboardPage />} />
                  <Route
                    path="/admin/customers"
                    element={
                      <RequireBillingPermission anyOf={[BillingPermissions.customersRead]}>
                        <CustomersPage />
                      </RequireBillingPermission>
                    }
                  />
                  <Route
                    path="/admin/packages"
                    element={
                      <RequireBillingPermission anyOf={[BillingPermissions.packagesRead]}>
                        <PackagesPage />
                      </RequireBillingPermission>
                    }
                  />
                  <Route
                    path="/admin/pppoe-accounts"
                    element={
                      <RequireBillingPermission anyOf={[BillingPermissions.pppoeRead]}>
                        <PppoeAccountsPage />
                      </RequireBillingPermission>
                    }
                  />
                  <Route
                    path="/admin/nas"
                    element={
                      <RequireBillingPermission anyOf={[BillingPermissions.nasRead]}>
                        <NasPage />
                      </RequireBillingPermission>
                    }
                  />
                  <Route
                    path="/admin/sessions"
                    element={
                      <RequireBillingPermission anyOf={[BillingPermissions.sessionsRead]}>
                        <SessionsPage />
                      </RequireBillingPermission>
                    }
                  />
                  <Route
                    path="/admin/invoices"
                    element={
                      <RequireBillingPermission anyOf={[BillingPermissions.invoicesRead]}>
                        <InvoicesPage />
                      </RequireBillingPermission>
                    }
                  />
                  <Route
                    path="/admin/reports"
                    element={
                      <RequireBillingPermission anyOf={[BillingPermissions.reportsRead]}>
                        <ReportsPage />
                      </RequireBillingPermission>
                    }
                  />
                  <Route
                    path="/admin/audit"
                    element={
                      <RequireBillingPermission anyOf={[BillingPermissions.auditRead]}>
                        <AuditPage />
                      </RequireBillingPermission>
                    }
                  />
                </Route>
                <Route
                  element={
                    <RequireAuth>
                      <Layout />
                    </RequireAuth>
                  }
                >
                  <Route path="/" element={<OrdersPage />} />
                  <Route path="/profile" element={<ProfilePage />} />
                </Route>
                <Route path="*" element={<Navigate to="/" replace />} />
              </Routes>
            </BrowserRouter>
          </BillingAuthProvider>
        </AuthProvider>
      </QueryClientProvider>
    </MotionConfig>
  );
}
