import { useState } from "react";
import { Link } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiError } from "../../../shared/lib/api";
import { formatDate, formatIDR } from "../../../shared/lib/billing-format";
import {
  Badge,
  Button,
  EmptyState,
  FormAlert,
  PageHeader,
  Select,
  Table,
  TBody,
  TD,
  TH,
  THead,
  TR,
} from "../../../shared/ui/billing-ui";
import { listPortalInvoices } from "../api";
import type { PortalInvoice } from "../api";
import { PayDialog } from "../components/PayDialog";

function statusTone(status: string): "success" | "warning" | "default" {
  if (status === "paid") {
    return "success";
  }
  if (status === "unpaid") {
    return "warning";
  }
  return "default";
}

export function PortalInvoicesPage() {
  const [status, setStatus] = useState("");
  const [paying, setPaying] = useState<PortalInvoice | null>(null);
  const [channelDown, setChannelDown] = useState(false);
  const queryClient = useQueryClient();

  const list = useQuery({
    queryKey: ["portal", "invoices", status],
    queryFn: () => listPortalInvoices(status === "" ? undefined : status),
  });

  function onPaid(): void {
    setPaying(null);
    void queryClient.invalidateQueries({ queryKey: ["portal", "invoices"] });
    void queryClient.invalidateQueries({ queryKey: ["portal", "summary"] });
  }

  return (
    <div>
      <PageHeader title="Tagihan" description="Tagihan dan riwayat pembayaran Anda." />
      {channelDown && (
        <div className="mb-4" role="alert">
          <FormAlert message="Pembayaran QRIS sedang tidak tersedia. Silakan bayar manual (tunai/transfer) lalu konfirmasi ke admin." />
        </div>
      )}
      <div className="flex flex-col sm:flex-row gap-2 mb-4">
        <label htmlFor="portal-invoices-status" className="sr-only">
          Saring status
        </label>
        <Select
          id="portal-invoices-status"
          value={status}
          onChange={(e) => setStatus(e.target.value)}
          className="sm:max-w-48"
        >
          <option value="">Semua status</option>
          <option value="unpaid">Belum lunas</option>
          <option value="paid">Lunas</option>
          <option value="void">Batal</option>
        </Select>
      </div>
      {list.isPending && (
        <p className="text-sm text-slate-500" role="status">
          Memuat tagihan…
        </p>
      )}
      {list.isError && (
        <div className="flex flex-col gap-3">
          <FormAlert
            message={list.error instanceof ApiError ? list.error.message : "gagal memuat tagihan"}
          />
          <div>
            <Button variant="secondary" size="sm" onClick={() => void list.refetch()}>
              Coba lagi
            </Button>
          </div>
        </div>
      )}
      {list.data && list.data.data.length === 0 && (
        <EmptyState title="Belum ada tagihan" hint="Tagihan baru akan muncul di sini." />
      )}
      {list.data && list.data.data.length > 0 && (
        <Table label="Daftar tagihan">
          <THead>
            <TR>
              <TH>Nomor</TH>
              <TH>Akun</TH>
              <TH align="right">Total</TH>
              <TH>Jatuh tempo</TH>
              <TH>Status</TH>
              <TH ariaLabel="aksi" align="right" />
            </TR>
          </THead>
          <TBody>
            {list.data.data.map((invoice) => {
              const unpaid = invoice.status === "unpaid";
              return (
                <TR key={invoice.id}>
                  <TD>
                    <span className="font-semibold">{invoice.number}</span>
                  </TD>
                  <TD muted>
                    {invoice.pppoe_username} · {invoice.package_name}
                  </TD>
                  <TD align="right">{formatIDR(invoice.total)}</TD>
                  <TD muted>{formatDate(invoice.due_date)}</TD>
                  <TD>
                    <Badge tone={statusTone(invoice.status)}>{invoice.status}</Badge>
                  </TD>
                  <TD align="right">
                    <div className="flex justify-end gap-2">
                      <Link
                        to={`/portal/invoices/${invoice.id}`}
                        className="inline-flex items-center justify-center gap-2 font-semibold rounded-md border text-sm px-3 py-1.5 min-h-[36px] bg-white border-slate-300 text-slate-800 hover:bg-slate-50 no-underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-700"
                      >
                        Detail
                      </Link>
                      {unpaid && !channelDown && (
                        <Button size="sm" onClick={() => setPaying(invoice)}>
                          {invoice.pending_attempt ? "Lanjutkan" : "Bayar"}
                        </Button>
                      )}
                    </div>
                  </TD>
                </TR>
              );
            })}
          </TBody>
        </Table>
      )}
      {paying !== null && (
        <PayDialog
          invoiceId={paying.id}
          invoiceNumber={paying.number}
          total={paying.total}
          pending={paying.pending_attempt}
          onClose={() => setPaying(null)}
          onPaid={onPaid}
          onChannelDown={() => {
            setPaying(null);
            setChannelDown(true);
          }}
        />
      )}
    </div>
  );
}
