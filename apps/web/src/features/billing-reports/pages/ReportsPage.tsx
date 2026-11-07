import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ApiError } from "../../../shared/lib/api";
import { formatBytes, formatDate, formatIDR } from "../../../shared/lib/billing-format";
import {
  Badge,
  Button,
  Card,
  EmptyState,
  Field,
  FormAlert,
  Input,
  PageHeader,
  Select,
  Table,
  TBody,
  TD,
  TH,
  THead,
  TR,
} from "../../../shared/ui/billing-ui";
import {
  getGrowthReport,
  getReceivablesReport,
  getRevenueReport,
  getUsageReport,
} from "../api";
import type { RevenueGroupBy } from "../api";
import { GrowthChart, RevenueBarChart, UsageLineChart } from "../components/ReportCharts";

type ReportTab = "revenue" | "usage" | "receivables" | "growth";

const TABS: { id: ReportTab; label: string }[] = [
  { id: "revenue", label: "Pendapatan" },
  { id: "usage", label: "Pemakaian" },
  { id: "receivables", label: "Piutang" },
  { id: "growth", label: "Pertumbuhan" },
];

function toISODate(date: Date): string {
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${date.getFullYear()}-${month}-${day}`;
}

function isoToday(): string {
  return toISODate(new Date());
}

function isoDaysAgo(days: number): string {
  const date = new Date();
  date.setDate(date.getDate() - days);
  return toISODate(date);
}

/** YYYY-MM-DD bisa dibandingkan leksikografis. Kosong = tanpa batas. */
function rangeValid(from: string, to: string): boolean {
  return from === "" || to === "" || from <= to;
}

function overdueTone(days: number): "danger" | "warning" | "default" {
  if (days > 30) {
    return "danger";
  }
  if (days > 7) {
    return "warning";
  }
  return "default";
}

export function ReportsPage() {
  const [tab, setTab] = useState<ReportTab>("revenue");
  const [revFrom, setRevFrom] = useState(() => isoDaysAgo(29));
  const [revTo, setRevTo] = useState(() => isoToday());
  const [revGroup, setRevGroup] = useState<RevenueGroupBy>("day");
  const [useFrom, setUseFrom] = useState(() => isoDaysAgo(29));
  const [useTo, setUseTo] = useState(() => isoToday());
  const [growthMonths, setGrowthMonths] = useState(6);

  const revenueValid = rangeValid(revFrom, revTo);
  const usageValid = rangeValid(useFrom, useTo);

  const revenue = useQuery({
    queryKey: ["billing", "reports", "revenue", revFrom, revTo, revGroup],
    queryFn: () =>
      getRevenueReport({
        from: revFrom === "" ? undefined : revFrom,
        to: revTo === "" ? undefined : revTo,
        group_by: revGroup,
      }),
    enabled: tab === "revenue" && revenueValid,
  });
  const usage = useQuery({
    queryKey: ["billing", "reports", "usage", useFrom, useTo],
    queryFn: () =>
      getUsageReport({
        from: useFrom === "" ? undefined : useFrom,
        to: useTo === "" ? undefined : useTo,
      }),
    enabled: tab === "usage" && usageValid,
  });
  const receivables = useQuery({
    queryKey: ["billing", "reports", "receivables"],
    queryFn: getReceivablesReport,
    enabled: tab === "receivables",
  });
  const growth = useQuery({
    queryKey: ["billing", "reports", "growth", growthMonths],
    queryFn: () => getGrowthReport(growthMonths),
    enabled: tab === "growth",
  });

  const growthTotals =
    growth.data !== undefined
      ? {
          added: growth.data.points.reduce((sum, p) => sum + p.new, 0),
          churn: growth.data.points.reduce((sum, p) => sum + p.churn, 0),
          active: growth.data.points[growth.data.points.length - 1]?.active ?? 0,
        }
      : null;

  return (
    <div>
      <PageHeader
        title="Laporan"
        description="Pendapatan, pemakaian, piutang, dan pertumbuhan pelanggan."
      />
      <div
        role="tablist"
        aria-label="Jenis laporan"
        className="mb-5 flex gap-1 overflow-x-auto rounded-lg border border-slate-300 bg-white p-1"
      >
        {TABS.map((item) => (
          <button
            key={item.id}
            type="button"
            role="tab"
            id={`reports-tab-${item.id}`}
            aria-selected={tab === item.id}
            aria-controls={`reports-panel-${item.id}`}
            onClick={() => setTab(item.id)}
            className={`min-h-[44px] flex-1 cursor-pointer rounded-md px-4 py-2 text-sm font-semibold whitespace-nowrap focus-visible:outline-2 focus-visible:outline-emerald-700 ${
              tab === item.id ? "bg-emerald-700 text-white" : "text-slate-600 hover:bg-slate-100"
            }`}
          >
            {item.label}
          </button>
        ))}
      </div>

      {tab === "revenue" && (
        <section role="tabpanel" id="reports-panel-revenue" aria-labelledby="reports-tab-revenue">
          <div className="mb-4 grid gap-3 sm:grid-cols-3">
            <Field label="Dari" htmlFor="revenue-from">
              <Input
                id="revenue-from"
                type="date"
                value={revFrom}
                max={revTo === "" ? undefined : revTo}
                onChange={(e) => setRevFrom(e.target.value)}
              />
            </Field>
            <Field label="Sampai" htmlFor="revenue-to">
              <Input
                id="revenue-to"
                type="date"
                value={revTo}
                min={revFrom === "" ? undefined : revFrom}
                onChange={(e) => setRevTo(e.target.value)}
              />
            </Field>
            <Field label="Kelompok" htmlFor="revenue-group">
              <Select
                id="revenue-group"
                value={revGroup}
                onChange={(e) => setRevGroup(e.target.value === "month" ? "month" : "day")}
              >
                <option value="day">Harian</option>
                <option value="month">Bulanan</option>
              </Select>
            </Field>
          </div>
          {!revenueValid && (
            <div className="mb-4">
              <FormAlert message="Tanggal awal harus sebelum atau sama dengan tanggal akhir." />
            </div>
          )}
          {revenue.isPending && (
            <p className="text-sm text-slate-500" role="status">
              Memuat laporan pendapatan…
            </p>
          )}
          {revenue.isError && (
            <div className="flex flex-col gap-3">
              <FormAlert
                message={
                  revenue.error instanceof ApiError
                    ? revenue.error.message
                    : "gagal memuat laporan pendapatan"
                }
              />
              <div>
                <Button variant="secondary" size="sm" onClick={() => void revenue.refetch()}>
                  Coba lagi
                </Button>
              </div>
            </div>
          )}
          {revenue.data && (
            <div className="flex flex-col gap-5">
              <div className="grid gap-3 sm:grid-cols-2">
                <Card>
                  <p className="m-0 text-sm text-slate-500">Total pendapatan</p>
                  <p className="m-0 mt-1 text-xl font-bold text-slate-900 tabular-nums">
                    {formatIDR(revenue.data.total)}
                  </p>
                </Card>
                <Card>
                  <p className="m-0 text-sm text-slate-500">Periode</p>
                  <p className="m-0 mt-1 text-xl font-bold text-slate-900 tabular-nums">
                    {revenue.data.points.length} {revGroup === "day" ? "hari" : "bulan"}
                  </p>
                </Card>
              </div>
              <RevenueBarChart points={revenue.data.points} />
              {revenue.data.points.length === 0 ? (
                <EmptyState title="Belum ada data" hint="Ubah rentang tanggal lalu coba lagi." />
              ) : (
                <Table label="Pendapatan per periode">
                  <THead>
                    <TR>
                      <TH>Periode</TH>
                      <TH align="right">Total</TH>
                    </TR>
                  </THead>
                  <TBody>
                    {revenue.data.points.map((point) => (
                      <TR key={point.period}>
                        <TD muted>{point.period}</TD>
                        <TD align="right">{formatIDR(point.total)}</TD>
                      </TR>
                    ))}
                  </TBody>
                </Table>
              )}
            </div>
          )}
        </section>
      )}

      {tab === "usage" && (
        <section role="tabpanel" id="reports-panel-usage" aria-labelledby="reports-tab-usage">
          <div className="mb-4 grid gap-3 sm:grid-cols-2">
            <Field label="Dari" htmlFor="usage-from">
              <Input
                id="usage-from"
                type="date"
                value={useFrom}
                max={useTo === "" ? undefined : useTo}
                onChange={(e) => setUseFrom(e.target.value)}
              />
            </Field>
            <Field label="Sampai" htmlFor="usage-to">
              <Input
                id="usage-to"
                type="date"
                value={useTo}
                min={useFrom === "" ? undefined : useFrom}
                onChange={(e) => setUseTo(e.target.value)}
              />
            </Field>
          </div>
          {!usageValid && (
            <div className="mb-4">
              <FormAlert message="Tanggal awal harus sebelum atau sama dengan tanggal akhir." />
            </div>
          )}
          {usage.isPending && (
            <p className="text-sm text-slate-500" role="status">
              Memuat laporan pemakaian…
            </p>
          )}
          {usage.isError && (
            <div className="flex flex-col gap-3">
              <FormAlert
                message={
                  usage.error instanceof ApiError
                    ? usage.error.message
                    : "gagal memuat laporan pemakaian"
                }
              />
              <div>
                <Button variant="secondary" size="sm" onClick={() => void usage.refetch()}>
                  Coba lagi
                </Button>
              </div>
            </div>
          )}
          {usage.data && (
            <div className="flex flex-col gap-5">
              <div className="grid gap-3 sm:grid-cols-2">
                <Card>
                  <p className="m-0 text-sm text-slate-500">Total unduh</p>
                  <p className="m-0 mt-1 text-xl font-bold text-slate-900 tabular-nums">
                    {formatBytes(usage.data.totals.bytes_out)}
                  </p>
                </Card>
                <Card>
                  <p className="m-0 text-sm text-slate-500">Total unggah</p>
                  <p className="m-0 mt-1 text-xl font-bold text-slate-900 tabular-nums">
                    {formatBytes(usage.data.totals.bytes_in)}
                  </p>
                </Card>
              </div>
              <UsageLineChart points={usage.data.points} />
              {usage.data.points.length === 0 ? (
                <EmptyState title="Belum ada data" hint="Ubah rentang tanggal lalu coba lagi." />
              ) : (
                <Table label="Pemakaian per tanggal">
                  <THead>
                    <TR>
                      <TH>Tanggal</TH>
                      <TH align="right">Unduh</TH>
                      <TH align="right">Unggah</TH>
                    </TR>
                  </THead>
                  <TBody>
                    {usage.data.points.map((point) => (
                      <TR key={point.date}>
                        <TD muted>{point.date}</TD>
                        <TD align="right">{formatBytes(point.bytes_out)}</TD>
                        <TD align="right">{formatBytes(point.bytes_in)}</TD>
                      </TR>
                    ))}
                  </TBody>
                </Table>
              )}
            </div>
          )}
        </section>
      )}

      {tab === "receivables" && (
        <section
          role="tabpanel"
          id="reports-panel-receivables"
          aria-labelledby="reports-tab-receivables"
        >
          {receivables.isPending && (
            <p className="text-sm text-slate-500" role="status">
              Memuat laporan piutang…
            </p>
          )}
          {receivables.isError && (
            <div className="flex flex-col gap-3">
              <FormAlert
                message={
                  receivables.error instanceof ApiError
                    ? receivables.error.message
                    : "gagal memuat laporan piutang"
                }
              />
              <div>
                <Button variant="secondary" size="sm" onClick={() => void receivables.refetch()}>
                  Coba lagi
                </Button>
              </div>
            </div>
          )}
          {receivables.data && (
            <div className="flex flex-col gap-5">
              <div className="grid gap-3 sm:grid-cols-2">
                <Card>
                  <p className="m-0 text-sm text-slate-500">Total piutang</p>
                  <p className="m-0 mt-1 text-xl font-bold text-slate-900 tabular-nums">
                    {formatIDR(receivables.data.total)}
                  </p>
                </Card>
                <Card>
                  <p className="m-0 text-sm text-slate-500">Invoice menunggak</p>
                  <p className="m-0 mt-1 text-xl font-bold text-slate-900 tabular-nums">
                    {receivables.data.invoices.length}
                  </p>
                </Card>
              </div>
              {receivables.data.invoices.length === 0 ? (
                <EmptyState title="Tidak ada piutang" hint="Semua invoice sudah lunas." />
              ) : (
                <Table label="Daftar piutang">
                  <THead>
                    <TR>
                      <TH>Nomor</TH>
                      <TH>Pelanggan</TH>
                      <TH align="right">Total</TH>
                      <TH>Jatuh tempo</TH>
                      <TH>Telat</TH>
                    </TR>
                  </THead>
                  <TBody>
                    {receivables.data.invoices.map((invoice) => (
                      <TR key={invoice.id}>
                        <TD>
                          <span className="font-semibold">{invoice.number}</span>
                        </TD>
                        <TD muted>{invoice.customer_name}</TD>
                        <TD align="right">{formatIDR(invoice.total)}</TD>
                        <TD muted>{formatDate(invoice.due_date)}</TD>
                        <TD>
                          <Badge tone={overdueTone(invoice.days_overdue)}>
                            {invoice.days_overdue} hari
                          </Badge>
                        </TD>
                      </TR>
                    ))}
                  </TBody>
                </Table>
              )}
            </div>
          )}
        </section>
      )}

      {tab === "growth" && (
        <section role="tabpanel" id="reports-panel-growth" aria-labelledby="reports-tab-growth">
          <div className="mb-4 grid gap-3 sm:max-w-64">
            <Field label="Rentang" htmlFor="growth-months">
              <Select
                id="growth-months"
                value={String(growthMonths)}
                onChange={(e) => setGrowthMonths(Number(e.target.value))}
              >
                <option value="3">3 bulan</option>
                <option value="6">6 bulan</option>
                <option value="12">12 bulan</option>
              </Select>
            </Field>
          </div>
          {growth.isPending && (
            <p className="text-sm text-slate-500" role="status">
              Memuat laporan pertumbuhan…
            </p>
          )}
          {growth.isError && (
            <div className="flex flex-col gap-3">
              <FormAlert
                message={
                  growth.error instanceof ApiError
                    ? growth.error.message
                    : "gagal memuat laporan pertumbuhan"
                }
              />
              <div>
                <Button variant="secondary" size="sm" onClick={() => void growth.refetch()}>
                  Coba lagi
                </Button>
              </div>
            </div>
          )}
          {growth.data && growthTotals && (
            <div className="flex flex-col gap-5">
              <div className="grid gap-3 sm:grid-cols-3">
                <Card>
                  <p className="m-0 text-sm text-slate-500">Pelanggan aktif</p>
                  <p className="m-0 mt-1 text-xl font-bold text-slate-900 tabular-nums">
                    {growthTotals.active}
                  </p>
                </Card>
                <Card>
                  <p className="m-0 text-sm text-slate-500">Pelanggan baru</p>
                  <p className="m-0 mt-1 text-xl font-bold tabular-nums text-emerald-800">
                    +{growthTotals.added}
                  </p>
                </Card>
                <Card>
                  <p className="m-0 text-sm text-slate-500">Churn</p>
                  <p className="m-0 mt-1 text-xl font-bold tabular-nums text-red-800">
                    −{growthTotals.churn}
                  </p>
                </Card>
              </div>
              <GrowthChart points={growth.data.points} />
              {growth.data.points.length === 0 ? (
                <EmptyState title="Belum ada data" hint="Ubah rentang bulan lalu coba lagi." />
              ) : (
                <Table label="Pertumbuhan per periode">
                  <THead>
                    <TR>
                      <TH>Periode</TH>
                      <TH align="right">Baru</TH>
                      <TH align="right">Churn</TH>
                      <TH align="right">Aktif</TH>
                    </TR>
                  </THead>
                  <TBody>
                    {growth.data.points.map((point) => (
                      <TR key={point.period}>
                        <TD muted>{point.period}</TD>
                        <TD align="right">{point.new}</TD>
                        <TD align="right">{point.churn}</TD>
                        <TD align="right">{point.active}</TD>
                      </TR>
                    ))}
                  </TBody>
                </Table>
              )}
            </div>
          )}
        </section>
      )}
    </div>
  );
}
