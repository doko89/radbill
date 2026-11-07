import { Link, NavLink, Outlet, useNavigate } from "react-router-dom";
import { Gauge } from "lucide-react";
import { useBillingAuth } from "../../features/billing-auth/BillingAuthProvider";

const NAV_ITEMS: { to: string; label: string; end: boolean }[] = [
  { to: "/admin", label: "Dasbor", end: true },
  { to: "/admin/customers", label: "Pelanggan", end: false },
  { to: "/admin/packages", label: "Paket", end: false },
  { to: "/admin/pppoe-accounts", label: "Akun PPPoE", end: false },
  { to: "/admin/nas", label: "NAS", end: false },
  { to: "/admin/sessions", label: "Sesi", end: false },
  { to: "/admin/invoices", label: "Tagihan", end: false },
  { to: "/admin/reports", label: "Laporan", end: false },
  { to: "/admin/audit", label: "Audit", end: false },
];

export function BillingLayout() {
  const auth = useBillingAuth();
  const navigate = useNavigate();

  async function onLogout(): Promise<void> {
    await auth.logout();
    navigate("/admin/login");
  }

  return (
    <div className="min-h-dvh bg-slate-100 text-slate-900">
      <a
        href="#konten-billing"
        className="sr-only focus:not-sr-only focus:absolute focus:z-[60] focus:m-2 focus:rounded-md focus:bg-white focus:px-4 focus:py-2 focus:shadow"
      >
        Lewati ke konten
      </a>
      <header className="bg-white border-b border-slate-200">
        <div className="max-w-6xl mx-auto px-4 py-3 flex items-center justify-between gap-3">
          <Link to="/admin" className="inline-flex items-center gap-2 font-bold text-lg text-slate-900 no-underline">
            <Gauge className="size-5 text-emerald-700" aria-hidden="true" />
            RadBill
            <span className="text-xs font-bold uppercase tracking-wide text-emerald-800 bg-emerald-100 rounded-full px-2 py-0.5">
              Admin
            </span>
          </Link>
          <div className="flex items-center gap-3 min-w-0">
            <p className="m-0 text-sm text-slate-500 truncate">
              {auth.user?.username}{" "}
              <span className="text-slate-400">({auth.user?.role})</span>
            </p>
            <button
              type="button"
              onClick={onLogout}
              className="shrink-0 inline-flex items-center justify-center gap-2 text-sm font-semibold rounded-md border border-slate-300 bg-white text-slate-800 px-3 py-1.5 min-h-[36px] hover:bg-slate-50 cursor-pointer focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-700"
            >
              Keluar
            </button>
          </div>
        </div>
      </header>
      <div className="max-w-6xl mx-auto px-4 py-6 flex flex-col md:flex-row gap-6">
        <nav
          aria-label="Navigasi billing"
          className="flex md:flex-col gap-1 overflow-x-auto md:overflow-visible md:w-52 shrink-0 md:sticky md:top-6 self-start w-full"
        >
          {NAV_ITEMS.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.end}
              className={({ isActive }) =>
                `whitespace-nowrap px-3 py-2 rounded-md text-sm font-semibold no-underline ${
                  isActive ? "bg-emerald-700 text-white" : "text-slate-700 hover:bg-slate-200"
                }`
              }
            >
              {item.label}
            </NavLink>
          ))}
        </nav>
        <main id="konten-billing" className="flex-1 min-w-0">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
