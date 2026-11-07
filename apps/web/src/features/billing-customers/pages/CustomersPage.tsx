import { useState } from "react";
import type { FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { ApiError } from "../../../shared/lib/api";
import { formatDateTime } from "../../../shared/lib/billing-format";
import {
  Button,
  ConfirmDialog,
  EmptyState,
  FormAlert,
  Input,
  PageHeader,
  Pagination,
  Table,
  TBody,
  TD,
  TH,
  THead,
  TR,
} from "../../../shared/ui/billing-ui";
import { CustomerDialog } from "../components/CustomerDialog";
import { customerAddress, customerName, customerPhone, deleteCustomer, listCustomers } from "../api";
import type { Customer } from "../api";

const PER_PAGE = 20;

export function CustomersPage() {
  const [searchInput, setSearchInput] = useState("");
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(1);
  const [dialog, setDialog] = useState<Customer | "new" | null>(null);
  const [deleting, setDeleting] = useState<Customer | null>(null);
  const queryClient = useQueryClient();

  const list = useQuery({
    queryKey: ["billing", "customers", search, page],
    queryFn: () =>
      listCustomers({ search: search === "" ? undefined : search, page, per_page: PER_PAGE }),
  });

  const removal = useMutation({
    mutationFn: (id: string) => deleteCustomer(id),
    onSuccess: () => {
      setDeleting(null);
      void queryClient.invalidateQueries({ queryKey: ["billing", "customers"] });
    },
  });

  function onSearch(event: FormEvent): void {
    event.preventDefault();
    setSearch(searchInput.trim());
    setPage(1);
  }

  function onSaved(): void {
    setDialog(null);
    void queryClient.invalidateQueries({ queryKey: ["billing", "customers"] });
  }

  return (
    <div>
      <PageHeader
        title="Pelanggan"
        description="CRUD pelanggan (nama, telepon, alamat, catatan)."
        actions={
          <Button size="sm" onClick={() => setDialog("new")}>
            <Plus className="size-4" aria-hidden="true" />
            Tambah pelanggan
          </Button>
        }
      />
      <form role="search" onSubmit={onSearch} className="flex gap-2 mb-4">
        <label htmlFor="customers-search" className="sr-only">
          Cari pelanggan
        </label>
        <Input
          id="customers-search"
          placeholder="Cari nama / telepon…"
          value={searchInput}
          onChange={(e) => setSearchInput(e.target.value)}
        />
        <Button type="submit" variant="secondary">
          Cari
        </Button>
      </form>
      {list.isPending && (
        <p className="text-sm text-slate-500" role="status">
          Memuat pelanggan…
        </p>
      )}
      {list.isError && (
        <div className="flex flex-col gap-3">
          <FormAlert
            message={
              list.error instanceof ApiError ? list.error.message : "gagal memuat pelanggan"
            }
          />
          <div>
            <Button variant="secondary" size="sm" onClick={() => void list.refetch()}>
              Coba lagi
            </Button>
          </div>
        </div>
      )}
      {removal.isError && (
        <div className="mb-4">
          <FormAlert
            message={
              removal.error instanceof ApiError ? removal.error.message : "gagal menghapus pelanggan"
            }
          />
        </div>
      )}
      {list.data && list.data.data.length === 0 && (
        <EmptyState title="Belum ada pelanggan" hint="Tambahkan pelanggan pertama Anda." />
      )}
      {list.data && list.data.data.length > 0 && (
        <>
          <Table label="Daftar pelanggan">
            <THead>
              <TR>
                <TH>Nama</TH>
                <TH>Telepon</TH>
                <TH>Alamat</TH>
                <TH>Dibuat</TH>
                <TH ariaLabel="aksi" align="right" />
              </TR>
            </THead>
            <TBody>
              {list.data.data.map((customer) => (
                <TR key={customer.id}>
                  <TD>
                    <span className="font-semibold">{customerName(customer)}</span>
                  </TD>
                  <TD muted>{customerPhone(customer) || "-"}</TD>
                  <TD muted>{customerAddress(customer) || "-"}</TD>
                  <TD muted>{formatDateTime(customer.created_at)}</TD>
                  <TD align="right">
                    <div className="flex justify-end gap-2">
                      <Button variant="secondary" size="sm" onClick={() => setDialog(customer)}>
                        <Pencil className="size-4" aria-hidden="true" />
                        Ubah
                      </Button>
                      <Button variant="danger" size="sm" onClick={() => setDeleting(customer)}>
                        <Trash2 className="size-4" aria-hidden="true" />
                        Hapus
                      </Button>
                    </div>
                  </TD>
                </TR>
              ))}
            </TBody>
          </Table>
          <Pagination page={page} perPage={PER_PAGE} total={list.data.total} onPage={setPage} />
        </>
      )}
      {dialog !== null && (
        <CustomerDialog
          initial={dialog === "new" ? null : dialog}
          onClose={() => setDialog(null)}
          onSaved={onSaved}
        />
      )}
      <ConfirmDialog
        open={deleting !== null}
        title="Hapus pelanggan?"
        message={`Pelanggan "${deleting ? customerName(deleting) : ""}" akan dihapus permanen.`}
        confirmLabel="Ya, hapus"
        danger
        busy={removal.isPending}
        onConfirm={() => {
          if (deleting) {
            removal.mutate(deleting.id);
          }
        }}
        onClose={() => setDeleting(null)}
      />
    </div>
  );
}
