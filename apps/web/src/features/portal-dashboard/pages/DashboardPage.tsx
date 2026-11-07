import { useState } from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { CircleAlert } from "lucide-react";
import { ApiError } from "../../../shared/lib/api";
import { formatBytes, formatDate, formatDuration, formatIDR } from "../../../shared/lib/billing-format";
import { Badge, Button, Card, EmptyState, FormAlert, PageHeader } from "../../../shared/ui/billing-ui";
import { TopupDialog } from "../../portal/components/TopupDialog";
import { getPortalSummary } from "../api";
import type { PortalAccount } from "../api";
import { QuotaMeter } from "../components/QuotaMeter";

function statusTone(status: string): "success" | "warning" | "danger" {
  if (status === "active") {
    return "success";
  }
  if (status === "isolated") {
    return "warning";
  }
  return "danger";
}

function statusLabel(status: string): string {
  if (status === "active") {
    return "Aktif";
  }
  if (status === "isolated") {
    return "Diisolir";
  }
  if (status === "disabled") {
    return "Nonaktif";
  }
  return status;
}

function AccountCard({
  account,
  isDefault,
  onTopup,
}: {
  account: PortalAccount;
  isDefault: boolean;
  onTopup: (accountId: string) => void;
}) {
  const remaining = account.remaining ?? null;
  const quotaLimit = remaining?.quota_bytes ?? null;
  const quotaUsed = remaining?.quota_used ?? null;
  const quotaLeft = quotaLimit !== null && quotaUsed !== null ? Math.max(0, quotaLimit - quotaUsed) : null;
  const timeLimit = remaining?.online_seconds ?? null;
  const timeUsed = remaining?.online_used ?? null;
  const timeLeft = timeLimit !== null && timeUsed !== null ? Math.max(0, timeLimit - timeUsed) : null;
  const quotaPackage =
    account.package.quota_bytes !== null && account.package.quota_bytes !== undefined;
  return (
    <Card className="flex flex-col gap-2">
      <div className="flex items-center justify-between gap-2">
        <p className="m-0 font-bold text-slate-900">{account.username}</p>
        <Badge tone={statusTone(account.status)}>{statusLabel(account.status)}</Badge>
      </div>
      <p className="m-0 text-sm text-slate-500">
        {account.package.name} · {formatIDR(account.package.price)}
        {isDefault && <span className="text-slate-400"> · utama</span>}
      </p>
      <p className="m-0 text-sm text-slate-500">
        Masa aktif: <span className="font-semibold text-slate-800">{formatDate(account.expires_at)}</span>
      </p>
      {remaining !== null && quotaUsed !== null && (
        <div className="border-t border-slate-100 pt-2">
          <QuotaMeter
            label={`Sisa kuota ${account.username}`}
            usedLabel={formatBytes(quotaUsed)}
            totalLabel={formatBytes(quotaLimit)}
            remainingLabel={formatBytes(quotaLeft)}
            used={quotaUsed}
            limit={quotaLimit}
            remaining={quotaLeft}
          />
        </div>
      )}
      {remaining !== null && timeUsed !== null && (
        <div className="border-t border-slate-100 pt-2">
          <QuotaMeter
            label={`Sisa waktu ${account.username}`}
            usedLabel={formatDuration(timeUsed)}
            totalLabel={formatDuration(timeLimit)}
            remainingLabel={formatDuration(timeLeft)}
            used={timeUsed}
            limit={timeLimit}
            remaining={timeLeft}
          />
        </div>
      )}
      <div className="flex flex-wrap gap-2 pt-1">
        {quotaPackage && (
          <Button variant="secondary" size="sm" onClick={() => onTopup(account.id)}>
            Top-Up kuota
          </Button>
        )}
        <Link
          to="/portal/packages"
          className="inline-flex min-h-[36px] items-center justify-center gap-2 rounded-md border border-slate-300 bg-white px-3 py-1.5 text-sm font-semibold text-slate-800 no-underline hover:bg-slate-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-700"
        >
          Kelola paket
        </Link>
      </div>
    </Card>
  );
}

export function PortalDashboardPage() {
  const summary = useQuery({ queryKey: ["portal", "summary"], queryFn: getPortalSummary });
  const [topupAccountId, setTopupAccountId] = useState<string | null>(null);

  return (
    <div>
      <PageHeader
        title="Dasbor"
        description={
          summary.data?.customer ? `Halo, ${summary.data.customer.name}.` : "Ringkasan layanan Anda."
        }
      />
      {summary.isPending && (
        <p className="text-sm text-slate-500" role="status">
          Memuat dasbor…
        </p>
      )}
      {summary.isError && (
        <div className="flex flex-col gap-3">
          <FormAlert
            message={summary.error instanceof ApiError ? summary.error.message : "gagal memuat dasbor"}
          />
          <div>
            <Button variant="secondary" size="sm" onClick={() => void summary.refetch()}>
              Coba lagi
            </Button>
          </div>
        </div>
      )}
      {summary.data && (
        <div className="flex flex-col gap-5">
          {summary.data.accounts.length === 0 ? (
            <EmptyState
              title="Belum ada layanan"
              hint="Hubungi admin untuk mengaktifkan akun internet Anda."
            />
          ) : (
            <section aria-label="Status layanan" className="grid gap-3 sm:grid-cols-2">
              {summary.data.accounts.map((account) => (
                <AccountCard
                  key={account.id}
                  account={account}
                  isDefault={account.id === summary.data?.default_account_id}
                  onTopup={setTopupAccountId}
                />
              ))}
            </section>
          )}

          <Card>
            <h2 className="m-0 mb-4 text-base font-bold text-slate-900">Sisa jatah</h2>
            <div className="flex flex-col gap-4">
              <QuotaMeter
                label="Kuota data"
                usedLabel={formatBytes(summary.data.quota.used_bytes)}
                totalLabel={formatBytes(summary.data.quota.limit_bytes)}
                remainingLabel={formatBytes(summary.data.quota.remaining_bytes)}
                used={summary.data.quota.used_bytes}
                limit={summary.data.quota.limit_bytes}
                remaining={summary.data.quota.remaining_bytes}
              />
              <QuotaMeter
                label="Waktu online"
                usedLabel={formatDuration(summary.data.online_time.used_seconds)}
                totalLabel={formatDuration(summary.data.online_time.limit_seconds)}
                remainingLabel={formatDuration(summary.data.online_time.remaining_seconds)}
                used={summary.data.online_time.used_seconds}
                limit={summary.data.online_time.limit_seconds}
                remaining={summary.data.online_time.remaining_seconds}
              />
            </div>
          </Card>

          <Card>
            <h2 className="m-0 mb-2 text-base font-bold text-slate-900">Jatuh tempo</h2>
            {summary.data.due ? (
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div className="flex items-start gap-2">
                  <CircleAlert className="size-5 mt-0.5 text-amber-600 shrink-0" aria-hidden="true" />
                  <div>
                    <p className="m-0 font-semibold text-slate-900 tabular-nums">
                      {summary.data.due.number} · {formatIDR(summary.data.due.total)}
                    </p>
                    <p className="m-0 text-sm text-slate-500">
                      Jatuh tempo {formatDate(summary.data.due.due_date)}
                      {summary.data.unpaid_count > 1 &&
                        ` · ${summary.data.unpaid_count} tagihan belum lunas (${formatIDR(summary.data.unpaid_total)})`}
                    </p>
                  </div>
                </div>
                <Link
                  to={`/portal/invoices/${summary.data.due.invoice_id}`}
                  className="inline-flex items-center justify-center gap-2 font-semibold rounded-md border text-sm px-4 py-2 min-h-[44px] bg-emerald-700 border-emerald-700 text-white hover:bg-emerald-800 no-underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-700"
                >
                  Bayar cepat
                </Link>
              </div>
            ) : (
              <p className="m-0 text-sm text-slate-500">
                Tidak ada tagihan belum lunas. Terima kasih!
              </p>
            )}
          </Card>
        </div>
      )}
      {topupAccountId !== null && summary.data && (
        <TopupDialog
          accounts={summary.data.accounts}
          defaultAccountId={topupAccountId}
          onClose={() => setTopupAccountId(null)}
        />
      )}
    </div>
  );
}
