import { useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import {
  Ban,
  Banknote,
  CircleDollarSign,
  TriangleAlert,
  UserCheck,
  UserX,
  Wallet,
  Wifi,
} from "lucide-react";
import { ApiError } from "../../../shared/lib/api";
import { formatDateTime, formatIDR } from "../../../shared/lib/billing-format";
import {
  Button,
  Card,
  EmptyState,
  FormAlert,
  PageHeader,
  Table,
  TBody,
  TD,
  TH,
  THead,
  TR,
} from "../../../shared/ui/billing-ui";
import { fetchAdminDashboard, fetchDashboardTotals } from "../api";

// Konten dasbor lama (Fase 1): dipindah ke bawah sebagai ringkasan,
// tautan dan halaman tujuannya tidak berubah.
const LEGACY_CARDS: {
  key: "customers" | "packages" | "pppoeAccounts" | "nas" | "onlineSessions" | "invoices";
  label: string;
  to: string;
  manage: string;
}[] = [
  { key: "customers", label: "Pelanggan", to: "/admin/customers", manage: "Kelola pelanggan" },
  { key: "packages", label: "Paket", to: "/admin/packages", manage: "Kelola paket" },
  { key: "pppoeAccounts", label: "Akun PPPoE", to: "/admin/pppoe-accounts", manage: "Kelola akun PPPoE" },
  { key: "nas", label: "NAS", to: "/admin/nas", manage: "Kelola NAS" },
  { key: "onlineSessions", label: "Sesi online", to: "/admin/sessions", manage: "Lihat sesi" },
  { key: "invoices", label: "Tagihan", to: "/admin/invoices", manage: "Kelola tagihan" },
];

function MetricCard({
  icon,
  label,
  value,
  pending,
  hint,
  to,
  accent,
}: {
  icon: ReactNode;
  label: string;
  value: string;
  pending: boolean;
  hint?: string | undefined;
  to?: string | undefined;
  accent: string;
}) {
  return (
    <Card className="flex flex-col gap-1">
      <div className="flex items-center gap-2">
        <span className={`inline-flex size-8 items-center justify-center rounded-md ${accent}`}>
          {icon}
        </span>
        <p className="m-0 text-sm font-semibold text-slate-500">{label}</p>
      </div>
      <p className="m-0 mt-1 text-3xl font-bold text-slate-900 tabular-nums" role="status">
        {pending ? "…" : value}
      </p>
      {hint && <p className="m-0 text-xs text-slate-500">{hint}</p>}
      {to && (
        <Link
          to={to}
          className="mt-1 text-sm font-semibold text-emerald-700 hover:underline w-fit"
        >
          Lihat detail
        </Link>
      )}
    </Card>
  );
}

export function DashboardPage() {
  const dashboard = useQuery({
    queryKey: ["billing", "dashboard", "g4"],
    queryFn: fetchAdminDashboard,
    refetchInterval: 60_000,
  });
  const totals = useQuery({ queryKey: ["billing", "dashboard"], queryFn: fetchDashboardTotals });

  const data = dashboard.data;
  const pending = dashboard.isPending;

  return (
    <div>
      <PageHeader
        title="Dasbor"
        description="Ringkasan operasional billing. Diperbarui otomatis tiap 60 detik."
        actions={
          <Button
            variant="secondary"
            size="sm"
            disabled={dashboard.isFetching}
            onClick={() => void dashboard.refetch()}
          >
            {dashboard.isFetching ? "Memuat…" : "Muat ulang"}
          </Button>
        }
      />

      {dashboard.isError && (
        <div className="mb-5 flex flex-col gap-3">
          <FormAlert
            message={
              dashboard.error instanceof ApiError
                ? dashboard.error.message
                : "gagal memuat dasbor"
            }
          />
          <div>
            <Button variant="secondary" size="sm" onClick={() => void dashboard.refetch()}>
              Coba lagi
            </Button>
          </div>
        </div>
      )}

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <MetricCard
          icon={<Wallet className="size-4" aria-hidden="true" />}
          label="Pendapatan bulan ini"
          value={formatIDR(data?.revenue_mtd ?? null)}
          pending={pending}
          accent="bg-emerald-100 text-emerald-800"
          to="/admin/reports"
          hint="Month-to-date"
        />
        <MetricCard
          icon={<Banknote className="size-4" aria-hidden="true" />}
          label="Piutang"
          value={formatIDR(data?.outstanding_total ?? null)}
          pending={pending}
          accent="bg-amber-100 text-amber-900"
          to="/admin/invoices"
          hint="Total invoice belum lunas"
        />
        <MetricCard
          icon={<TriangleAlert className="size-4" aria-hidden="true" />}
          label="Invoice menunggak"
          value={
            data === undefined ? "…" : data.overdue_count.toLocaleString("id-ID")
          }
          pending={pending}
          accent="bg-red-100 text-red-800"
          to="/admin/invoices"
          hint="Lewat jatuh tempo"
        />
        <MetricCard
          icon={<Wifi className="size-4" aria-hidden="true" />}
          label="Sesi online"
          value={
            data === undefined ? "…" : data.online_sessions.toLocaleString("id-ID")
          }
          pending={pending}
          accent="bg-sky-100 text-sky-900"
          to="/admin/sessions"
        />
      </div>

      <div className="mt-4 grid grid-cols-1 sm:grid-cols-3 gap-4">
        <MetricCard
          icon={<UserCheck className="size-4" aria-hidden="true" />}
          label="Akun aktif"
          value={
            data === undefined ? "…" : data.accounts.active.toLocaleString("id-ID")
          }
          pending={pending}
          accent="bg-emerald-100 text-emerald-800"
          to="/admin/pppoe-accounts"
        />
        <MetricCard
          icon={<UserX className="size-4" aria-hidden="true" />}
          label="Akun terisolir"
          value={
            data === undefined ? "…" : data.accounts.isolated.toLocaleString("id-ID")
          }
          pending={pending}
          accent="bg-amber-100 text-amber-900"
          to="/admin/pppoe-accounts"
        />
        <MetricCard
          icon={<Ban className="size-4" aria-hidden="true" />}
          label="Akun nonaktif"
          value={
            data === undefined ? "…" : data.accounts.disabled.toLocaleString("id-ID")
          }
          pending={pending}
          accent="bg-slate-200 text-slate-700"
          to="/admin/pppoe-accounts"
        />
      </div>

      {!dashboard.isError && (
        <section aria-labelledby="recent-payments-heading" className="mt-8">
          <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
            <h2 id="recent-payments-heading" className="m-0 text-lg font-bold text-slate-900">
              Pembayaran terkini
            </h2>
            {data && (
              <p className="m-0 text-xs text-slate-500">
                Diperbarui {formatDateTime(new Date(dashboard.dataUpdatedAt).toISOString())}
              </p>
            )}
          </div>
          {pending && (
            <p className="text-sm text-slate-500" role="status">
              Memuat pembayaran terkini…
            </p>
          )}
          {data && data.recent_payments.length === 0 && (
            <EmptyState title="Belum ada pembayaran" hint="Pembayaran yang lunas akan tampil di sini." />
          )}
          {data && data.recent_payments.length > 0 && (
            <Table label="Pembayaran terkini">
              <THead>
                <TR>
                  <TH>Nomor invoice</TH>
                  <TH align="right">Jumlah</TH>
                  <TH>Dibayar pada</TH>
                </TR>
              </THead>
              <TBody>
                {data.recent_payments.map((payment) => (
                  <TR key={payment.id}>
                    <TD>
                      <span className="font-semibold">{payment.number}</span>
                    </TD>
                    <TD align="right">
                      <span className="inline-flex items-center gap-1.5 tabular-nums">
                        <CircleDollarSign className="size-4 text-emerald-700" aria-hidden="true" />
                        {formatIDR(payment.amount)}
                      </span>
                    </TD>
                    <TD muted>{formatDateTime(payment.paid_at)}</TD>
                  </TR>
                ))}
              </TBody>
            </Table>
          )}
        </section>
      )}

      <section aria-labelledby="legacy-totals-heading" className="mt-8">
        <h2 id="legacy-totals-heading" className="m-0 mb-3 text-lg font-bold text-slate-900">
          Ringkasan data
        </h2>
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {LEGACY_CARDS.map((card) => {
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
      </section>
    </div>
  );
}
