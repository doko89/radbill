import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { Card, PageHeader } from "../../../shared/ui/billing-ui";
import { fetchDashboardTotals } from "../api";

const CARDS: { key: "customers" | "packages" | "pppoeAccounts" | "nas" | "onlineSessions" | "invoices"; label: string; to: string; manage: string }[] = [
  { key: "customers", label: "Pelanggan", to: "/admin/customers", manage: "Kelola pelanggan" },
  { key: "packages", label: "Paket", to: "/admin/packages", manage: "Kelola paket" },
  { key: "pppoeAccounts", label: "Akun PPPoE", to: "/admin/pppoe-accounts", manage: "Kelola akun PPPoE" },
  { key: "nas", label: "NAS", to: "/admin/nas", manage: "Kelola NAS" },
  { key: "onlineSessions", label: "Sesi online", to: "/admin/sessions", manage: "Lihat sesi" },
  { key: "invoices", label: "Tagihan", to: "/admin/invoices", manage: "Kelola tagihan" },
];

export function DashboardPage() {
  const totals = useQuery({ queryKey: ["billing", "dashboard"], queryFn: fetchDashboardTotals });

  return (
    <div>
      <PageHeader
        title="Dasbor"
        description="Ringkasan operasional billing (Fase 1). Angka tampil otomatis saat API /api/v1 tersedia."
      />
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
        {CARDS.map((card) => {
          const value = totals.data?.[card.key] ?? null;
          return (
            <Card key={card.key}>
              <p className="m-0 text-sm font-semibold text-slate-500">{card.label}</p>
              <p className="m-0 mt-1 text-3xl font-bold text-slate-900 tabular-nums" role="status">
                {totals.isPending ? "…" : value === null ? "-" : value.toLocaleString("id-ID")}
              </p>
              <Link
                to={card.to}
                className="inline-block mt-3 text-sm font-semibold text-emerald-700 hover:underline"
              >
                {card.manage}
              </Link>
            </Card>
          );
        })}
      </div>
      {totals.isError && (
        <p className="text-sm text-slate-500 mt-4" role="status">
          API billing belum dapat dijangkau — angka akan terisi setelah backend /api/v1 berjalan.
        </p>
      )}
    </div>
  );
}
