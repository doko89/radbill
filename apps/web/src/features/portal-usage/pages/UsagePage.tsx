import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ApiError } from "../../../shared/lib/api";
import { formatBytes, formatDate, formatDuration } from "../../../shared/lib/billing-format";
import {
  Button,
  Card,
  FormAlert,
  PageHeader,
  Table,
  TBody,
  TD,
  TH,
  THead,
  TR,
} from "../../../shared/ui/billing-ui";
import { getPortalUsage } from "../api";
import { UsageChart } from "../components/UsageChart";

const RANGES = [7, 30, 90] as const;

export function PortalUsagePage() {
  const [days, setDays] = useState<number>(30);
  const usage = useQuery({ queryKey: ["portal", "usage", days], queryFn: () => getPortalUsage(days) });

  return (
    <div>
      <PageHeader
        title="Pemakaian"
        description="Grafik dan riwayat pemakaian harian Anda."
        actions={
          <div className="flex gap-1 rounded-md border border-slate-300 bg-white p-1" role="group" aria-label="Rentang hari">
            {RANGES.map((range) => (
              <button
                key={range}
                type="button"
                onClick={() => setDays(range)}
                aria-pressed={days === range}
                className={`px-3 py-1.5 min-h-[36px] text-sm font-semibold rounded cursor-pointer focus-visible:outline-2 focus-visible:outline-emerald-700 ${
                  days === range ? "bg-emerald-700 text-white" : "text-slate-600 hover:bg-slate-100"
                }`}
              >
                {range} hari
              </button>
            ))}
          </div>
        }
      />
      {usage.isPending && (
        <p className="text-sm text-slate-500" role="status">
          Memuat pemakaian…
        </p>
      )}
      {usage.isError && (
        <div className="flex flex-col gap-3">
          <FormAlert
            message={usage.error instanceof ApiError ? usage.error.message : "gagal memuat pemakaian"}
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
          <div className="grid gap-3 sm:grid-cols-3">
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
            <Card>
              <p className="m-0 text-sm text-slate-500">Waktu online</p>
              <p className="m-0 mt-1 text-xl font-bold text-slate-900 tabular-nums">
                {formatDuration(usage.data.totals.online_seconds)}
              </p>
            </Card>
          </div>
          <UsageChart points={usage.data.points} />
          <details className="bg-white border border-slate-200 rounded-lg">
            <summary className="px-4 py-3 text-sm font-semibold text-slate-800 cursor-pointer">
              Lihat data tabel ({usage.data.points.length} hari)
            </summary>
            <div className="px-4 pb-4">
              <Table label="Pemakaian harian">
                <THead>
                  <TR>
                    <TH>Tanggal</TH>
                    <TH align="right">Unduh</TH>
                    <TH align="right">Unggah</TH>
                    <TH align="right">Online</TH>
                  </TR>
                </THead>
                <TBody>
                  {usage.data.points.map((point) => (
                    <TR key={point.date}>
                      <TD muted>{formatDate(point.date)}</TD>
                      <TD align="right">{formatBytes(point.bytes_out)}</TD>
                      <TD align="right">{formatBytes(point.bytes_in)}</TD>
                      <TD align="right" muted>
                        {formatDuration(point.online_seconds)}
                      </TD>
                    </TR>
                  ))}
                </TBody>
              </Table>
            </div>
          </details>
        </div>
      )}
    </div>
  );
}
