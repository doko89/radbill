import { useState } from "react";
import { Link } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiError } from "../../../shared/lib/api";
import {
  formatBytes,
  formatDate,
  formatDateTime,
  formatDuration,
  formatIDR,
} from "../../../shared/lib/billing-format";
import {
  Badge,
  Button,
  Card,
  ConfirmDialog,
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
import { TopupDialog } from "../../portal/components/TopupDialog";
import {
  cancelPortalPlanChange,
  getPortalSummary,
  listPortalPackages,
  listPortalPlanChanges,
  planChangeFromLabel,
  planChangeToLabel,
  portalPublicPackageName,
  portalPublicPackagePrice,
} from "../api";
import type { PortalAccount, PortalPublicPackage } from "../api";
import { PlanChangeDialog } from "../components/PlanChangeDialog";
import type { PlanDirection } from "../components/PlanChangeDialog";

function accountStatusTone(status: string): "success" | "warning" | "danger" {
  if (status === "active") {
    return "success";
  }
  if (status === "isolated") {
    return "warning";
  }
  return "danger";
}

function accountStatusLabel(status: string): string {
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

function changeStatusTone(status: string): "success" | "warning" | "danger" | "default" {
  const s = status.toLowerCase();
  if (s === "pending" || s === "processing" || s === "requested") {
    return "warning";
  }
  if (s === "approved" || s === "applied" || s === "active" || s === "completed" || s === "done") {
    return "success";
  }
  if (
    s === "rejected" ||
    s === "cancelled" ||
    s === "canceled" ||
    s === "failed" ||
    s === "expired"
  ) {
    return "danger";
  }
  return "default";
}

function isCancellable(status: string): boolean {
  return status.toLowerCase() === "pending";
}

function isNotFound(err: unknown): boolean {
  return err instanceof ApiError && err.status === 404;
}

function hasQuota(account: PortalAccount): boolean {
  return account.package.quota_bytes !== null && account.package.quota_bytes !== undefined;
}

/** Grup upgrade paket aktif: dari summary bila ada, else cocokkan id di daftar publik. */
function currentGroup(account: PortalAccount, list: PortalPublicPackage[]): string | null {
  const fromSummary = account.package.upgrade_group ?? null;
  if (fromSummary !== null && fromSummary !== "") {
    return fromSummary;
  }
  const match = list.find((pkg) => pkg.id === account.package.id);
  const group = match?.upgrade_group ?? null;
  return group !== null && group !== "" ? group : null;
}

function eligibleTargets(
  account: PortalAccount,
  list: PortalPublicPackage[],
): PortalPublicPackage[] | null {
  const group = currentGroup(account, list);
  if (group === null) {
    return null;
  }
  return list.filter(
    (pkg) =>
      pkg.id !== account.package.id &&
      pkg.allow_self_service === true &&
      (pkg.upgrade_group ?? null) === group,
  );
}

function directionFor(account: PortalAccount, target: PortalPublicPackage): PlanDirection {
  const targetPrice = portalPublicPackagePrice(target);
  if (targetPrice === null) {
    return "unknown";
  }
  return targetPrice > account.package.price ? "upgrade" : "downgrade";
}

function priceDeltaLabel(account: PortalAccount, target: PortalPublicPackage): string | null {
  const targetPrice = portalPublicPackagePrice(target);
  if (targetPrice === null) {
    return null;
  }
  const delta = targetPrice - account.package.price;
  if (delta === 0) {
    return "harga sama";
  }
  const sign = delta > 0 ? "+" : "−";
  return `${sign}${formatIDR(Math.abs(delta))}`;
}

function packageMeta(pkg: { rate_limit?: string | null | undefined }): string | null {
  if (pkg.rate_limit === null || pkg.rate_limit === undefined || pkg.rate_limit === "") {
    return null;
  }
  return pkg.rate_limit;
}

export function PortalPackagesPage() {
  const queryClient = useQueryClient();
  const summary = useQuery({ queryKey: ["portal", "summary"], queryFn: getPortalSummary });
  const packages = useQuery({ queryKey: ["portal", "packages"], queryFn: listPortalPackages });
  const history = useQuery({ queryKey: ["portal", "plan-changes"], queryFn: listPortalPlanChanges });

  const [topupAccountId, setTopupAccountId] = useState<string | null>(null);
  const [changing, setChanging] = useState<{ account: PortalAccount; target: PortalPublicPackage } | null>(null);
  const [cancelId, setCancelId] = useState<string | null>(null);
  const [cancelBusy, setCancelBusy] = useState(false);
  const [cancelError, setCancelError] = useState<string | null>(null);

  async function confirmCancel(): Promise<void> {
    if (cancelId === null) {
      return;
    }
    setCancelBusy(true);
    setCancelError(null);
    try {
      await cancelPortalPlanChange(cancelId);
      setCancelId(null);
      void queryClient.invalidateQueries({ queryKey: ["portal", "plan-changes"] });
      void queryClient.invalidateQueries({ queryKey: ["portal", "summary"] });
    } catch (err) {
      setCancelId(null);
      setCancelError(err instanceof ApiError ? err.message : "gagal membatalkan pengajuan");
    } finally {
      setCancelBusy(false);
    }
  }

  const hasArrears = (summary.data?.unpaid_count ?? 0) > 0;

  return (
    <div>
      <PageHeader
        title="Paket"
        description="Paket aktif, ganti paket mandiri, dan riwayat pengajuan."
      />
      {summary.isPending && (
        <p className="text-sm text-slate-500" role="status">
          Memuat paket…
        </p>
      )}
      {summary.isError && (
        <div className="flex flex-col gap-3">
          <FormAlert
            message={summary.error instanceof ApiError ? summary.error.message : "gagal memuat paket"}
          />
          <div>
            <Button variant="secondary" size="sm" onClick={() => void summary.refetch()}>
              Coba lagi
            </Button>
          </div>
        </div>
      )}
      {summary.data && summary.data.accounts.length === 0 && (
        <EmptyState
          title="Belum ada layanan"
          hint="Hubungi admin untuk mengaktifkan akun internet Anda."
        />
      )}
      {summary.data && summary.data.accounts.length > 0 && (
        <div className="flex flex-col gap-5">
          <section aria-label="Paket saya" className="flex flex-col gap-3">
            {summary.data.accounts.map((account) => {
              const targets = eligibleTargets(account, packages.data?.data ?? []);
              const rate = packageMeta(account.package);
              return (
                <Card key={account.id} className="flex flex-col gap-3">
                  <div className="flex items-center justify-between gap-2">
                    <p className="m-0 font-bold text-slate-900">{account.username}</p>
                    <Badge tone={accountStatusTone(account.status)}>
                      {accountStatusLabel(account.status)}
                    </Badge>
                  </div>
                  <div>
                    <p className="m-0 font-semibold text-slate-900">
                      {account.package.name} ·{" "}
                      <span className="tabular-nums">{formatIDR(account.package.price)}</span>
                    </p>
                    <p className="m-0 mt-1 text-sm text-slate-500">
                      {[
                        rate,
                        account.package.quota_bytes !== null
                          ? `kuota ${formatBytes(account.package.quota_bytes)}`
                          : "tanpa kuota",
                        account.package.online_seconds_limit !== null
                          ? `online ${formatDuration(account.package.online_seconds_limit)}`
                          : null,
                        `masa aktif ${formatDate(account.expires_at)}`,
                      ]
                        .filter((part): part is string => part !== null)
                        .join(" · ")}
                    </p>
                  </div>
                  {hasQuota(account) && (
                    <div>
                      <Button
                        variant="secondary"
                        size="sm"
                        onClick={() => setTopupAccountId(account.id)}
                      >
                        Top-Up kuota
                      </Button>
                    </div>
                  )}
                  <div className="border-t border-slate-100 pt-3">
                    <h2 className="m-0 mb-2 text-sm font-bold text-slate-900">Ganti paket</h2>
                    {hasArrears ? (
                      <p className="m-0 text-sm text-slate-600">
                        Ganti paket diblokir: masih ada{" "}
                        <span className="font-bold">
                          {summary.data?.unpaid_count ?? 0} tagihan
                        </span>{" "}
                        belum lunas.{" "}
                        <Link to="/portal/invoices" className="font-semibold text-emerald-800 underline">
                          Lunasi dulu
                        </Link>
                      </p>
                    ) : packages.isPending ? (
                      <p className="m-0 text-sm text-slate-500" role="status">
                        Memuat daftar paket…
                      </p>
                    ) : packages.isError ? (
                      isNotFound(packages.error) ? (
                        <p className="m-0 text-sm text-slate-600">
                          Daftar paket mandiri belum tersedia. Hubungi admin untuk ganti paket.
                        </p>
                      ) : (
                        <div className="flex flex-col gap-2">
                          <FormAlert
                            message={
                              packages.error instanceof ApiError
                                ? packages.error.message
                                : "gagal memuat daftar paket"
                            }
                          />
                          <div>
                            <Button
                              variant="secondary"
                              size="sm"
                              onClick={() => void packages.refetch()}
                            >
                              Coba lagi
                            </Button>
                          </div>
                        </div>
                      )
                    ) : targets === null ? (
                      <p className="m-0 text-sm text-slate-600">
                        Grup paket Anda tidak dikenali. Hubungi admin untuk ganti paket.
                      </p>
                    ) : targets.length === 0 ? (
                      <p className="m-0 text-sm text-slate-600">
                        Tidak ada paket tujuan dalam grup yang sama. Hubungi admin bila butuh paket
                        lain.
                      </p>
                    ) : (
                      <ul className="m-0 flex list-none flex-col gap-2 p-0">
                        {targets.map((target) => {
                          const direction = directionFor(account, target);
                          const delta = priceDeltaLabel(account, target);
                          return (
                            <li
                              key={target.id}
                              className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-slate-200 bg-slate-50 px-3 py-2"
                            >
                              <div className="min-w-0">
                                <p className="m-0 font-semibold text-slate-900">
                                  {portalPublicPackageName(target)}{" "}
                                  <span className="font-normal text-slate-500 tabular-nums">
                                    {formatIDR(portalPublicPackagePrice(target))}
                                    {delta !== null && ` (${delta})`}
                                  </span>
                                </p>
                                <p className="m-0 mt-0.5 text-xs text-slate-500">
                                  {[
                                    packageMeta(target),
                                    target.quota_bytes !== null && target.quota_bytes !== undefined
                                      ? `kuota ${formatBytes(target.quota_bytes)}`
                                      : "tanpa kuota",
                                  ]
                                    .filter((part): part is string => part !== null)
                                    .join(" · ")}
                                </p>
                              </div>
                              <div className="flex items-center gap-2">
                                <Badge
                                  tone={
                                    direction === "upgrade"
                                      ? "info"
                                      : direction === "downgrade"
                                        ? "warning"
                                        : "default"
                                  }
                                >
                                  {direction === "upgrade"
                                    ? "Upgrade"
                                    : direction === "downgrade"
                                      ? "Downgrade"
                                      : "Ganti"}
                                </Badge>
                                <Button
                                  size="sm"
                                  onClick={() => setChanging({ account, target })}
                                >
                                  Pilih
                                </Button>
                              </div>
                            </li>
                          );
                        })}
                      </ul>
                    )}
                  </div>
                </Card>
              );
            })}
          </section>

          <section aria-label="Riwayat ganti paket">
            <h2 className="m-0 mb-2 text-base font-bold text-slate-900">Riwayat ganti paket</h2>
            {cancelError && (
              <div className="mb-3">
                <FormAlert message={cancelError} />
              </div>
            )}
            {history.isPending && (
              <p className="text-sm text-slate-500" role="status">
                Memuat riwayat…
              </p>
            )}
            {history.isError && (
              <div className="flex flex-col gap-3">
                <FormAlert
                  message={
                    history.error instanceof ApiError
                      ? history.error.message
                      : "gagal memuat riwayat"
                  }
                />
                <div>
                  <Button variant="secondary" size="sm" onClick={() => void history.refetch()}>
                    Coba lagi
                  </Button>
                </div>
              </div>
            )}
            {history.data && history.data.length === 0 && (
              <EmptyState title="Belum ada pengajuan" hint="Pengajuan ganti paket tampil di sini." />
            )}
            {history.data && history.data.length > 0 && (
              <Table label="Riwayat ganti paket">
                <THead>
                  <TR>
                    <TH>Diajukan</TH>
                    <TH>Akun</TH>
                    <TH>Perubahan</TH>
                    <TH>Status</TH>
                    <TH>Berlaku</TH>
                    <TH>Prorata</TH>
                    <TH ariaLabel="aksi" align="right" />
                  </TR>
                </THead>
                <TBody>
                  {history.data.map((change) => (
                    <TR key={change.id}>
                      <TD muted>{formatDateTime(change.created_at)}</TD>
                      <TD muted>{change.pppoe_username ?? "-"}</TD>
                      <TD>
                        <span className="font-medium">
                          {planChangeFromLabel(change) || "-"} →{" "}
                          {planChangeToLabel(change) || "-"}
                        </span>
                      </TD>
                      <TD>
                        <Badge tone={changeStatusTone(change.status)}>{change.status}</Badge>
                      </TD>
                      <TD muted>{formatDateTime(change.effective_at)}</TD>
                      <TD>
                        {change.prorata_invoice_id ? (
                          <Link
                            to={`/portal/invoices/${change.prorata_invoice_id}`}
                            state={{ autoPay: true }}
                            className="font-semibold text-emerald-800 underline"
                          >
                            Bayar
                          </Link>
                        ) : (
                          <span className="text-slate-400">-</span>
                        )}
                      </TD>
                      <TD align="right">
                        {isCancellable(change.status) && (
                          <Button variant="secondary" size="sm" onClick={() => setCancelId(change.id)}>
                            Batalkan
                          </Button>
                        )}
                      </TD>
                    </TR>
                  ))}
                </TBody>
              </Table>
            )}
          </section>
        </div>
      )}
      {topupAccountId !== null && summary.data && (
        <TopupDialog
          accounts={summary.data.accounts}
          defaultAccountId={topupAccountId}
          onClose={() => setTopupAccountId(null)}
        />
      )}
      {changing !== null && (
        <PlanChangeDialog
          account={changing.account}
          target={changing.target}
          targetPrice={portalPublicPackagePrice(changing.target)}
          direction={directionFor(changing.account, changing.target)}
          hasArrears={hasArrears}
          unpaidCount={summary.data?.unpaid_count ?? 0}
          onClose={() => setChanging(null)}
        />
      )}
      <ConfirmDialog
        open={cancelId !== null}
        title="Batalkan pengajuan?"
        message="Pengajuan ganti paket yang dibatalkan tidak diproses. Paket aktif tidak berubah."
        confirmLabel="Ya, batalkan"
        busy={cancelBusy}
        onConfirm={() => void confirmCancel()}
        onClose={() => {
          if (!cancelBusy) {
            setCancelId(null);
          }
        }}
      />
    </div>
  );
}
