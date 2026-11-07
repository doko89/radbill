import { useState } from "react";
import type { FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Pencil, Plus, Settings2, Trash2 } from "lucide-react";
import { ApiError } from "../../../shared/lib/api";
import { formatDate } from "../../../shared/lib/billing-format";
import {
  Badge,
  Button,
  ConfirmDialog,
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
import { PppoeAccountDialog } from "../components/PppoeAccountDialog";
import { PppoeManageDialog } from "../components/PppoeManageDialog";
import {
  deletePppoeAccount,
  listPppoeAccounts,
  pppoeCustomerName,
  pppoePackageName,
} from "../api";
import type { PppoeAccount } from "../api";

const PER_PAGE = 20;

function statusTone(status: string | null | undefined): "success" | "warning" | "danger" | "default" {
  if (status === "active") {
    return "success";
  }
  if (status === "isolated") {
    return "warning";
  }
  if (status === "disabled") {
    return "danger";
  }
  return "default";
}

export function PppoeAccountsPage() {
  const [searchInput, setSearchInput] = useState("");
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("");
  const [page, setPage] = useState(1);
  const [dialog, setDialog] = useState<PppoeAccount | "new" | null>(null);
  const [managing, setManaging] = useState<PppoeAccount | null>(null);
  const [deleting, setDeleting] = useState<PppoeAccount | null>(null);
  const queryClient = useQueryClient();

  const list = useQuery({
    queryKey: ["billing", "pppoe-accounts", search, status, page],
    queryFn: () =>
      listPppoeAccounts({
        search: search === "" ? undefined : search,
        status: status === "" ? undefined : status,
        page,
        per_page: PER_PAGE,
      }),
  });

  const removal = useMutation({
    mutationFn: (id: string) => deletePppoeAccount(id),
    onSuccess: () => {
      setDeleting(null);
      void queryClient.invalidateQueries({ queryKey: ["billing", "pppoe-accounts"] });
    },
  });

  function onSearch(event: FormEvent): void {
    event.preventDefault();
    setSearch(searchInput.trim());
    setPage(1);
  }

  function onSaved(): void {
    setDialog(null);
    void queryClient.invalidateQueries({ queryKey: ["billing", "pppoe-accounts"] });
  }

  return (
    <div>
      <PageHeader
        title="Akun PPPoE"
        description="Siklus hidup akun: isolir, aktifkan, nonaktifkan, perpanjang, ganti paket, reset kuota, disconnect."
        actions={
          <Button size="sm" onClick={() => setDialog("new")}>
            <Plus className="size-4" aria-hidden="true" />
            Tambah akun
          </Button>
        }
      />
      <form role="search" onSubmit={onSearch} className="flex flex-col sm:flex-row gap-2 mb-4">
        <label htmlFor="pppoe-search" className="sr-only">
          Cari akun PPPoE
        </label>
        <Input
          id="pppoe-search"
          placeholder="Cari username…"
          value={searchInput}
          onChange={(e) => setSearchInput(e.target.value)}
        />
        <label htmlFor="pppoe-status-filter" className="sr-only">
          Saring status
        </label>
        <Select
          id="pppoe-status-filter"
          value={status}
          onChange={(e) => {
            setStatus(e.target.value);
            setPage(1);
          }}
          className="sm:max-w-48"
        >
          <option value="">Semua status</option>
          <option value="active">active</option>
          <option value="isolated">isolated</option>
          <option value="disabled">disabled</option>
        </Select>
        <Button type="submit" variant="secondary">
          Cari
        </Button>
      </form>
      {list.isPending && (
        <p className="text-sm text-slate-500" role="status">
          Memuat akun PPPoE…
        </p>
      )}
      {list.isError && (
        <div className="flex flex-col gap-3">
          <FormAlert
            message={list.error instanceof ApiError ? list.error.message : "gagal memuat akun PPPoE"}
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
              removal.error instanceof ApiError ? removal.error.message : "gagal menghapus akun"
            }
          />
        </div>
      )}
      {list.data && list.data.data.length === 0 && (
        <EmptyState title="Belum ada akun PPPoE" hint="Tambahkan akun pertama Anda." />
      )}
      {list.data && list.data.data.length > 0 && (
        <>
          <Table label="Daftar akun PPPoE">
            <THead>
              <TR>
                <TH>Username</TH>
                <TH>Pelanggan</TH>
                <TH>Paket</TH>
                <TH>Status</TH>
                <TH>Berakhir</TH>
                <TH ariaLabel="aksi" align="right" />
              </TR>
            </THead>
            <TBody>
              {list.data.data.map((account) => (
                <TR key={account.id}>
                  <TD>
                    <span className="font-semibold font-mono text-[13px]">
                      {account.username ?? account.id}
                    </span>
                  </TD>
                  <TD muted>{pppoeCustomerName(account) || "-"}</TD>
                  <TD muted>{pppoePackageName(account) || "-"}</TD>
                  <TD>
                    <Badge tone={statusTone(account.status)}>{account.status ?? "?"}</Badge>
                  </TD>
                  <TD muted>{formatDate(account.expires_at)}</TD>
                  <TD align="right">
                    <div className="flex justify-end gap-2">
                      <Button variant="secondary" size="sm" onClick={() => setManaging(account)}>
                        <Settings2 className="size-4" aria-hidden="true" />
                        Kelola
                      </Button>
                      <Button variant="secondary" size="sm" onClick={() => setDialog(account)}>
                        <Pencil className="size-4" aria-hidden="true" />
                        Ubah
                      </Button>
                      <Button variant="danger" size="sm" onClick={() => setDeleting(account)}>
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
        <PppoeAccountDialog
          initial={dialog === "new" ? null : dialog}
          onClose={() => setDialog(null)}
          onSaved={onSaved}
        />
      )}
      {managing !== null && (
        <PppoeManageDialog account={managing} onClose={() => setManaging(null)} />
      )}
      <ConfirmDialog
        open={deleting !== null}
        title="Hapus akun PPPoE?"
        message={`Akun "${deleting?.username ?? ""}" akan dihapus permanen.`}
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
