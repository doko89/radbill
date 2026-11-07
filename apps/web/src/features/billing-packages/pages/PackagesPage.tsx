import { useState } from "react";
import type { FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { ApiError } from "../../../shared/lib/api";
import { formatBytes, formatDuration, formatIDR } from "../../../shared/lib/billing-format";
import {
  Badge,
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
import { PackageDialog } from "../components/PackageDialog";
import {
  deletePackage,
  listPackages,
  packageName,
  packagePeriod,
  packagePrice,
  packageTypeLabel,
} from "../api";
import type { BillingPackage } from "../api";

const PER_PAGE = 20;

export function PackagesPage() {
  const [searchInput, setSearchInput] = useState("");
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(1);
  const [dialog, setDialog] = useState<BillingPackage | "new" | null>(null);
  const [deleting, setDeleting] = useState<BillingPackage | null>(null);
  const queryClient = useQueryClient();

  const list = useQuery({
    queryKey: ["billing", "packages", search, page],
    queryFn: () =>
      listPackages({ search: search === "" ? undefined : search, page, per_page: PER_PAGE }),
  });

  const removal = useMutation({
    mutationFn: (id: string) => deletePackage(id),
    onSuccess: () => {
      setDeleting(null);
      void queryClient.invalidateQueries({ queryKey: ["billing", "packages"] });
    },
  });

  function onSearch(event: FormEvent): void {
    event.preventDefault();
    setSearch(searchInput.trim());
    setPage(1);
  }

  function onSaved(): void {
    setDialog(null);
    void queryClient.invalidateQueries({ queryKey: ["billing", "packages"] });
  }

  return (
    <div>
      <PageHeader
        title="Paket"
        description="Kombinasi kecepatan, kuota, waktu online, dan FUP (PRD §6.2)."
        actions={
          <Button size="sm" onClick={() => setDialog("new")}>
            <Plus className="size-4" aria-hidden="true" />
            Tambah paket
          </Button>
        }
      />
      <form role="search" onSubmit={onSearch} className="flex gap-2 mb-4">
        <label htmlFor="packages-search" className="sr-only">
          Cari paket
        </label>
        <Input
          id="packages-search"
          placeholder="Cari nama paket…"
          value={searchInput}
          onChange={(e) => setSearchInput(e.target.value)}
        />
        <Button type="submit" variant="secondary">
          Cari
        </Button>
      </form>
      {list.isPending && (
        <p className="text-sm text-slate-500" role="status">
          Memuat paket…
        </p>
      )}
      {list.isError && (
        <div className="flex flex-col gap-3">
          <FormAlert
            message={list.error instanceof ApiError ? list.error.message : "gagal memuat paket"}
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
              removal.error instanceof ApiError ? removal.error.message : "gagal menghapus paket"
            }
          />
        </div>
      )}
      {list.data && list.data.data.length === 0 && (
        <EmptyState title="Belum ada paket" hint="Tambahkan paket pertama Anda." />
      )}
      {list.data && list.data.data.length > 0 && (
        <>
          <Table label="Daftar paket">
            <THead>
              <TR>
                <TH>Nama</TH>
                <TH>Harga</TH>
                <TH>Tipe</TH>
                <TH>Kuota</TH>
                <TH>Validity</TH>
                <TH>Periode</TH>
                <TH ariaLabel="aksi" align="right" />
              </TR>
            </THead>
            <TBody>
              {list.data.data.map((pkg) => (
                <TR key={pkg.id}>
                  <TD>
                    <span className="font-semibold">{packageName(pkg)}</span>{" "}
                    {pkg.is_public === true && <Badge tone="info">publik</Badge>}
                  </TD>
                  <TD muted>{formatIDR(packagePrice(pkg))}</TD>
                  <TD muted>{packageTypeLabel(pkg)}</TD>
                  <TD muted>
                    {pkg.quota_bytes === null || pkg.quota_bytes === undefined
                      ? "tanpa batas"
                      : formatBytes(pkg.quota_bytes)}
                  </TD>
                  <TD muted>{formatDuration(pkg.validity_seconds)}</TD>
                  <TD muted>{packagePeriod(pkg) || "-"}</TD>
                  <TD align="right">
                    <div className="flex justify-end gap-2">
                      <Button variant="secondary" size="sm" onClick={() => setDialog(pkg)}>
                        <Pencil className="size-4" aria-hidden="true" />
                        Ubah
                      </Button>
                      <Button variant="danger" size="sm" onClick={() => setDeleting(pkg)}>
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
        <PackageDialog
          initial={dialog === "new" ? null : dialog}
          onClose={() => setDialog(null)}
          onSaved={onSaved}
        />
      )}
      <ConfirmDialog
        open={deleting !== null}
        title="Hapus paket?"
        message={`Paket "${deleting ? packageName(deleting) : ""}" akan dihapus permanen.`}
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
