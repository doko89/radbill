import { useState } from "react";
import type { FormEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { Download, ScrollText } from "lucide-react";
import { ApiError } from "../../../shared/lib/api";
import { formatDateTime } from "../../../shared/lib/billing-format";
import {
  Badge,
  Button,
  EmptyState,
  Field,
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
import { AUDIT_ACTIONS, PAGE_SIZE, downloadAuditCsv, listAuditLogs } from "../api";

type AuditFilters = {
  actor: string;
  action: string;
  from: string;
  to: string;
};

const EMPTY_FILTERS: AuditFilters = { actor: "", action: "", from: "", to: "" };

function actionTone(action: string): "success" | "danger" | "warning" | "info" | "default" {
  if (
    action.endsWith(".deleted") ||
    action.endsWith(".voided") ||
    action.endsWith(".disabled") ||
    action.endsWith(".isolated") ||
    action.endsWith(".cancelled")
  ) {
    return "danger";
  }
  if (action.endsWith(".created") || action.endsWith(".activated") || action === "payment.manual") {
    return "success";
  }
  if (action === "pppoe.password_reveal") {
    return "warning";
  }
  if (
    action.endsWith(".updated") ||
    action === "settings.updated" ||
    action.startsWith("auth.")
  ) {
    return "info";
  }
  return "default";
}

/** YYYY-MM-DD bisa dibandingkan leksikografis. Kosong = tanpa batas. */
function rangeValid(from: string, to: string): boolean {
  return from === "" || to === "" || from <= to;
}

export function AuditPage() {
  const [draft, setDraft] = useState<AuditFilters>(EMPTY_FILTERS);
  const [filters, setFilters] = useState<AuditFilters>(EMPTY_FILTERS);
  const [page, setPage] = useState(1);
  const [csvBusy, setCsvBusy] = useState(false);
  const [csvError, setCsvError] = useState<string | null>(null);

  const valid = rangeValid(filters.from, filters.to);

  const logs = useQuery({
    queryKey: ["billing", "audit", filters, page],
    queryFn: () =>
      listAuditLogs({
        actor: filters.actor === "" ? undefined : filters.actor,
        action: filters.action === "" ? undefined : filters.action,
        from: filters.from === "" ? undefined : filters.from,
        to: filters.to === "" ? undefined : filters.to,
        page,
      }),
    enabled: valid,
  });

  function onApply(event: FormEvent): void {
    event.preventDefault();
    setFilters(draft);
    setPage(1);
  }

  function onReset(): void {
    setDraft(EMPTY_FILTERS);
    setFilters(EMPTY_FILTERS);
    setPage(1);
  }

  async function onDownloadCsv(): Promise<void> {
    setCsvBusy(true);
    setCsvError(null);
    try {
      await downloadAuditCsv({
        actor: filters.actor === "" ? undefined : filters.actor,
        action: filters.action === "" ? undefined : filters.action,
        from: filters.from === "" ? undefined : filters.from,
        to: filters.to === "" ? undefined : filters.to,
      });
    } catch (error) {
      setCsvError(error instanceof ApiError ? error.message : "unduh CSV gagal");
    } finally {
      setCsvBusy(false);
    }
  }

  return (
    <div>
      <PageHeader
        title="Audit log"
        description="Jejak aksi admin: perubahan data, pembayaran manual, dan penampilan sandi."
        actions={
          <Button
            variant="secondary"
            size="sm"
            disabled={csvBusy || !valid}
            onClick={() => void onDownloadCsv()}
          >
            <Download className="size-4" aria-hidden="true" />
            {csvBusy ? "Mengunduh…" : "Unduh CSV"}
          </Button>
        }
      />

      <form onSubmit={onApply} className="mb-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <Field label="Aktor" htmlFor="audit-actor" hint="Username atau id akun">
          <Input
            id="audit-actor"
            placeholder="mis. admin"
            value={draft.actor}
            onChange={(e) => setDraft({ ...draft, actor: e.target.value })}
          />
        </Field>
        <Field label="Aksi" htmlFor="audit-action" hint="Prefix didukung, mis. pppoe">
          <Input
            id="audit-action"
            placeholder="mis. invoice.voided"
            list="audit-action-suggestions"
            value={draft.action}
            onChange={(e) => setDraft({ ...draft, action: e.target.value })}
          />
          <datalist id="audit-action-suggestions">
            {AUDIT_ACTIONS.map((action) => (
              <option key={action} value={action} />
            ))}
          </datalist>
        </Field>
        <Field label="Dari tanggal" htmlFor="audit-from">
          <Input
            id="audit-from"
            type="date"
            value={draft.from}
            max={draft.to === "" ? undefined : draft.to}
            onChange={(e) => setDraft({ ...draft, from: e.target.value })}
          />
        </Field>
        <Field label="Sampai tanggal" htmlFor="audit-to">
          <Input
            id="audit-to"
            type="date"
            value={draft.to}
            min={draft.from === "" ? undefined : draft.from}
            onChange={(e) => setDraft({ ...draft, to: e.target.value })}
          />
        </Field>
        <div className="flex items-end gap-2 sm:col-span-2 lg:col-span-4">
          <Button type="submit" size="sm">
            Terapkan filter
          </Button>
          <Button type="button" variant="secondary" size="sm" onClick={onReset}>
            Atur ulang
          </Button>
        </div>
      </form>

      {!valid && (
        <div className="mb-4">
          <FormAlert message="Tanggal awal harus sebelum atau sama dengan tanggal akhir." />
        </div>
      )}
      {csvError && (
        <div className="mb-4">
          <FormAlert message={csvError} />
        </div>
      )}

      {logs.isPending && (
        <p className="text-sm text-slate-500" role="status">
          Memuat audit log…
        </p>
      )}
      {logs.isError && (
        <div className="flex flex-col gap-3">
          <FormAlert
            message={
              logs.error instanceof ApiError ? logs.error.message : "gagal memuat audit log"
            }
          />
          <div>
            <Button variant="secondary" size="sm" onClick={() => void logs.refetch()}>
              Coba lagi
            </Button>
          </div>
        </div>
      )}
      {logs.data && logs.data.data.length === 0 && (
        <EmptyState
          title="Belum ada entri audit"
          hint="Ubah filter atau rentang tanggal lalu coba lagi."
        />
      )}
      {logs.data && logs.data.data.length > 0 && (
        <>
          <Table label="Audit log admin">
            <THead>
              <TR>
                <TH>Waktu</TH>
                <TH>Aktor</TH>
                <TH>Aksi</TH>
                <TH>Target</TH>
                <TH>Detail</TH>
                <TH>IP</TH>
              </TR>
            </THead>
            <TBody>
              {logs.data.data.map((entry) => (
                <TR key={entry.id}>
                  <TD muted>
                    <span className="whitespace-nowrap">{formatDateTime(entry.created_at)}</span>
                  </TD>
                  <TD>
                    <span className="font-semibold">{entry.actor_username || "-"}</span>
                  </TD>
                  <TD>
                    <Badge tone={actionTone(entry.action)}>
                      <span className="inline-flex items-center gap-1">
                        <ScrollText className="size-3" aria-hidden="true" />
                        {entry.action}
                      </span>
                    </Badge>
                  </TD>
                  <TD muted>
                    <span className="font-mono text-[13px] whitespace-nowrap">
                      {entry.target_type}
                      {entry.target_id ? `:${entry.target_id.slice(0, 8)}` : ""}
                    </span>
                  </TD>
                  <TD muted>
                    <span
                      className="block max-w-64 truncate"
                      title={entry.detail === "" ? undefined : entry.detail}
                    >
                      {entry.detail === "" ? "-" : entry.detail}
                    </span>
                  </TD>
                  <TD muted>
                    <span className="font-mono text-[13px]">{entry.ip === "" ? "-" : entry.ip}</span>
                  </TD>
                </TR>
              ))}
            </TBody>
          </Table>
          <Pagination page={page} perPage={PAGE_SIZE} total={logs.data.total} onPage={setPage} />
        </>
      )}
    </div>
  );
}
