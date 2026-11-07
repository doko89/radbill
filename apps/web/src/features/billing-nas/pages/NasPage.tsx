import { useState } from "react";
import type { FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Pencil, PlugZap, Plus, Trash2 } from "lucide-react";
import { ApiError } from "../../../shared/lib/api";
import {
  Button,
  ConfirmDialog,
  EmptyState,
  FormAlert,
  FormNote,
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
import { NasDialog } from "../components/NasDialog";
import { deleteNas, listNas, nasIp, nasName, testNasCoa } from "../api";
import type { Nas } from "../api";

const PER_PAGE = 20;

export function NasPage() {
  const [searchInput, setSearchInput] = useState("");
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(1);
  const [dialog, setDialog] = useState<Nas | "new" | null>(null);
  const [deleting, setDeleting] = useState<Nas | null>(null);
  const [coaNote, setCoaNote] = useState<string | null>(null);
  const [coaError, setCoaError] = useState<string | null>(null);
  const queryClient = useQueryClient();

  const list = useQuery({
    queryKey: ["billing", "nas", search, page],
    queryFn: () => listNas({ search: search === "" ? undefined : search, page, per_page: PER_PAGE }),
  });

  const removal = useMutation({
    mutationFn: (id: string) => deleteNas(id),
    onSuccess: () => {
      setDeleting(null);
      void queryClient.invalidateQueries({ queryKey: ["billing", "nas"] });
    },
  });

  const coaTest = useMutation({
    mutationFn: (id: string) => testNasCoa(id),
    onSuccess: (result) => {
      if (result.ok) {
        setCoaNote(result.message);
        setCoaError(null);
      } else {
        setCoaError(result.message);
        setCoaNote(null);
      }
    },
    onError: (error: unknown) => {
      setCoaError(error instanceof ApiError ? error.message : "tes CoA gagal");
      setCoaNote(null);
    },
  });

  function onSearch(event: FormEvent): void {
    event.preventDefault();
    setSearch(searchInput.trim());
    setPage(1);
  }

  function onSaved(): void {
    setDialog(null);
    void queryClient.invalidateQueries({ queryKey: ["billing", "nas"] });
  }

  return (
    <div>
      <PageHeader
        title="NAS"
        description="Router RADIUS client + tes CoA (UDP 3799)."
        actions={
          <Button size="sm" onClick={() => setDialog("new")}>
            <Plus className="size-4" aria-hidden="true" />
            Tambah NAS
          </Button>
        }
      />
      <form role="search" onSubmit={onSearch} className="flex gap-2 mb-4">
        <label htmlFor="nas-search" className="sr-only">
          Cari NAS
        </label>
        <Input
          id="nas-search"
          placeholder="Cari nama / IP…"
          value={searchInput}
          onChange={(e) => setSearchInput(e.target.value)}
        />
        <Button type="submit" variant="secondary">
          Cari
        </Button>
      </form>
      {coaNote && (
        <div className="mb-4">
          <FormNote message={coaNote} />
        </div>
      )}
      {coaError && (
        <div className="mb-4">
          <FormAlert message={coaError} />
        </div>
      )}
      {list.isPending && (
        <p className="text-sm text-slate-500" role="status">
          Memuat NAS…
        </p>
      )}
      {list.isError && (
        <div className="flex flex-col gap-3">
          <FormAlert
            message={list.error instanceof ApiError ? list.error.message : "gagal memuat NAS"}
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
              removal.error instanceof ApiError ? removal.error.message : "gagal menghapus NAS"
            }
          />
        </div>
      )}
      {list.data && list.data.data.length === 0 && (
        <EmptyState title="Belum ada NAS" hint="Tambahkan router pertama Anda." />
      )}
      {list.data && list.data.data.length > 0 && (
        <>
          <Table label="Daftar NAS">
            <THead>
              <TR>
                <TH>Nama</TH>
                <TH>IP</TH>
                <TH>Port CoA</TH>
                <TH ariaLabel="aksi" align="right" />
              </TR>
            </THead>
            <TBody>
              {list.data.data.map((nas) => (
                <TR key={nas.id}>
                  <TD>
                    <span className="font-semibold">{nasName(nas)}</span>
                  </TD>
                  <TD muted>
                    <span className="font-mono text-[13px]">{nasIp(nas) || "-"}</span>
                  </TD>
                  <TD muted>{nas.coa_port ?? "-"}</TD>
                  <TD align="right">
                    <div className="flex justify-end gap-2">
                      <Button
                        variant="secondary"
                        size="sm"
                        disabled={coaTest.isPending}
                        onClick={() => coaTest.mutate(nas.id)}
                      >
                        <PlugZap className="size-4" aria-hidden="true" />
                        {coaTest.isPending && coaTest.variables === nas.id
                          ? "Mengetes…"
                          : "Tes CoA"}
                      </Button>
                      <Button variant="secondary" size="sm" onClick={() => setDialog(nas)}>
                        <Pencil className="size-4" aria-hidden="true" />
                        Ubah
                      </Button>
                      <Button variant="danger" size="sm" onClick={() => setDeleting(nas)}>
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
        <NasDialog
          initial={dialog === "new" ? null : dialog}
          onClose={() => setDialog(null)}
          onSaved={onSaved}
        />
      )}
      <ConfirmDialog
        open={deleting !== null}
        title="Hapus NAS?"
        message={`NAS "${deleting ? nasName(deleting) : ""}" akan dihapus permanen.`}
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
