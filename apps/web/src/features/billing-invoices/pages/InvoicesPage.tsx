import { useState } from "react";
import type { FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Eye, Plus } from "lucide-react";
import { ApiError } from "../../../shared/lib/api";
import { formatDate, formatIDR } from "../../../shared/lib/billing-format";
import {
  Badge,
  Button,
  EmptyState,
  FormAlert,
  Input,
  PageHeader,
  Pagination,
  Select,
  Table,
  TBody,
  TD,
  TH,
  THead,
  TR,
} from "../../../shared/ui/billing-ui";
import { InvoiceDetailDialog } from "../components/InvoiceDetailDialog";
import { InvoiceFormDialog } from "../components/InvoiceFormDialog";
import { InvoicePaymentDialog } from "../components/InvoicePaymentDialog";
import { invoiceAccountLabel, invoiceNumber, invoiceTotal, listInvoices } from "../api";
import type { Invoice } from "../api";

const PER_PAGE = 20;

function statusTone(status: string | null | undefined): "success" | "warning" | "default" {
  if (status === "paid") {
    return "success";
  }
  if (status === "unpaid") {
    return "warning";
  }
  return "default";
}

export function InvoicesPage() {
  const [searchInput, setSearchInput] = useState("");
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("");
  const [page, setPage] = useState(1);
  const [creating, setCreating] = useState(false);
  const [detailId, setDetailId] = useState<string | null>(null);
  const [payment, setPayment] = useState<{ invoice: Invoice; mode: "pay" | "void" } | null>(null);
  const queryClient = useQueryClient();

  const list = useQuery({
    queryKey: ["billing", "invoices", search, status, page],
    queryFn: () =>
      listInvoices({
        search: search === "" ? undefined : search,
        status: status === "" ? undefined : status,
        page,
        per_page: PER_PAGE,
      }),
  });

  function onSearch(event: FormEvent): void {
    event.preventDefault();
    setSearch(searchInput.trim());
    setPage(1);
  }

  function onSaved(): void {
    setCreating(false);
    setPayment(null);
    void queryClient.invalidateQueries({ queryKey: ["billing", "invoices"] });
  }

  return (
    <div>
      <PageHeader
        title="Tagihan"
        description="Invoice per akun PPPoE + pembayaran manual (tunai/transfer)."
        actions={
          <Button size="sm" onClick={() => setCreating(true)}>
            <Plus className="size-4" aria-hidden="true" />
            Buat tagihan
          </Button>
        }
      />
      <form role="search" onSubmit={onSearch} className="flex flex-col sm:flex-row gap-2 mb-4">
        <label htmlFor="invoices-search" className="sr-only">
          Cari tagihan
        </label>
        <Input
          id="invoices-search"
          placeholder="Cari nomor tagihan…"
          value={searchInput}
          onChange={(e) => setSearchInput(e.target.value)}
        />
        <label htmlFor="invoices-status-filter" className="sr-only">
          Saring status
        </label>
        <Select
          id="invoices-status-filter"
          value={status}
          onChange={(e) => {
            setStatus(e.target.value);
            setPage(1);
          }}
          className="sm:max-w-48"
        >
          <option value="">Semua status</option>
          <option value="unpaid">unpaid</option>
          <option value="paid">paid</option>
          <option value="void">void</option>
        </Select>
        <Button type="submit" variant="secondary">
          Cari
        </Button>
      </form>
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
        <EmptyState title="Belum ada tagihan" hint="Buat tagihan pertama Anda." />
      )}
      {list.data && list.data.data.length > 0 && (
        <>
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
                      <span className="font-semibold">{invoiceNumber(invoice)}</span>
                    </TD>
                    <TD muted>{invoiceAccountLabel(invoice) || "-"}</TD>
                    <TD align="right">{formatIDR(invoiceTotal(invoice))}</TD>
                    <TD muted>{formatDate(invoice.due_date)}</TD>
                    <TD>
                      <Badge tone={statusTone(invoice.status)}>{invoice.status ?? "?"}</Badge>
                    </TD>
                    <TD align="right">
                      <div className="flex justify-end gap-2">
                        <Button variant="secondary" size="sm" onClick={() => setDetailId(invoice.id)}>
                          <Eye className="size-4" aria-hidden="true" />
                          Detail
                        </Button>
                        {unpaid && (
                          <>
                            <Button
                              variant="secondary"
                              size="sm"
                              onClick={() => setPayment({ invoice, mode: "pay" })}
                            >
                              Bayar manual
                            </Button>
                            <Button
                              variant="danger"
                              size="sm"
                              onClick={() => setPayment({ invoice, mode: "void" })}
                            >
                              Void
                            </Button>
                          </>
                        )}
                      </div>
                    </TD>
                  </TR>
                );
              })}
            </TBody>
          </Table>
          <Pagination page={page} perPage={PER_PAGE} total={list.data.total} onPage={setPage} />
        </>
      )}
      {creating && <InvoiceFormDialog onClose={() => setCreating(false)} onSaved={onSaved} />}
      {detailId !== null && (
        <InvoiceDetailDialog id={detailId} onClose={() => setDetailId(null)} />
      )}
      {payment !== null && (
        <InvoicePaymentDialog
          invoice={payment.invoice}
          mode={payment.mode}
          onClose={() => setPayment(null)}
          onSaved={onSaved}
        />
      )}
    </div>
  );
}
