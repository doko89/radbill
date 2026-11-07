import { useState } from "react";
import type { FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { PhoneOff } from "lucide-react";
import { ApiError } from "../../../shared/lib/api";
import { formatBytes, formatDateTime, formatDuration } from "../../../shared/lib/billing-format";
import {
  Button,
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
import {
  disconnectSessionByAccount,
  listOnlineSessions,
  listSessionHistory,
  sessionBytesIn,
  sessionBytesOut,
  sessionDuration,
  sessionIp,
  sessionKey,
  sessionNas,
  sessionStarted,
  sessionStopped,
} from "../api";

const PER_PAGE = 20;

type Tab = "online" | "history";

function totalTraffic(inBytes: number | null, outBytes: number | null): string {
  if (inBytes === null && outBytes === null) {
    return "-";
  }
  return `↓ ${formatBytes(inBytes)} / ↑ ${formatBytes(outBytes)}`;
}

export function SessionsPage() {
  const [tab, setTab] = useState<Tab>("online");
  const [usernameInput, setUsernameInput] = useState("");
  const [username, setUsername] = useState("");
  const [page, setPage] = useState(1);
  const [notice, setNotice] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const queryClient = useQueryClient();

  const online = useQuery({
    queryKey: ["billing", "sessions", "online"],
    queryFn: () => listOnlineSessions(),
    refetchInterval: 15_000,
    enabled: tab === "online",
  });

  const history = useQuery({
    queryKey: ["billing", "sessions", "history", username, page],
    queryFn: () =>
      listSessionHistory({
        username: username === "" ? undefined : username,
        page,
        per_page: PER_PAGE,
      }),
    enabled: tab === "history",
  });

  const disconnect = useMutation({
    mutationFn: (pppoeAccountId: string) => disconnectSessionByAccount(pppoeAccountId),
    onSuccess: () => {
      setNotice("Permintaan disconnect terkirim.");
      setActionError(null);
      void queryClient.invalidateQueries({ queryKey: ["billing", "sessions", "online"] });
    },
    onError: (error: unknown) => {
      setActionError(error instanceof ApiError ? error.message : "disconnect gagal");
      setNotice(null);
    },
  });

  function onFilter(event: FormEvent): void {
    event.preventDefault();
    setUsername(usernameInput.trim());
    setPage(1);
  }

  const active = tab === "online" ? online : history;

  return (
    <div>
      <PageHeader
        title="Sesi"
        description="Sesi online dari accounting RADIUS dan riwayat sesi."
        actions={
          <Button variant="secondary" size="sm" onClick={() => void active.refetch()}>
            Muat ulang
          </Button>
        }
      />
      <div className="flex gap-2 mb-4" role="tablist" aria-label="Jenis sesi">
        <button
          type="button"
          role="tab"
          aria-selected={tab === "online"}
          onClick={() => setTab("online")}
          className={`px-4 py-2 rounded-md text-sm font-semibold cursor-pointer border ${
            tab === "online"
              ? "bg-emerald-700 border-emerald-700 text-white"
              : "bg-white border-slate-300 text-slate-700 hover:bg-slate-50"
          }`}
        >
          Online{online.data ? ` (${online.data.total})` : ""}
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={tab === "history"}
          onClick={() => setTab("history")}
          className={`px-4 py-2 rounded-md text-sm font-semibold cursor-pointer border ${
            tab === "history"
              ? "bg-emerald-700 border-emerald-700 text-white"
              : "bg-white border-slate-300 text-slate-700 hover:bg-slate-50"
          }`}
        >
          Riwayat
        </button>
      </div>

      {tab === "history" && (
        <form role="search" onSubmit={onFilter} className="flex gap-2 mb-4">
          <label htmlFor="sessions-username" className="sr-only">
            Saring username
          </label>
          <Input
            id="sessions-username"
            placeholder="Saring username…"
            value={usernameInput}
            onChange={(e) => setUsernameInput(e.target.value)}
          />
          <Button type="submit" variant="secondary">
            Saring
          </Button>
        </form>
      )}

      {notice && (
        <div className="mb-4">
          <FormNote message={notice} />
        </div>
      )}
      {actionError && (
        <div className="mb-4">
          <FormAlert message={actionError} />
        </div>
      )}
      {active.isPending && (
        <p className="text-sm text-slate-500" role="status">
          Memuat sesi…
        </p>
      )}
      {active.isError && (
        <div className="flex flex-col gap-3">
          <FormAlert
            message={active.error instanceof ApiError ? active.error.message : "gagal memuat sesi"}
          />
          <div>
            <Button variant="secondary" size="sm" onClick={() => void active.refetch()}>
              Coba lagi
            </Button>
          </div>
        </div>
      )}

      {tab === "online" && online.data && online.data.data.length === 0 && (
        <EmptyState title="Tidak ada sesi online" hint="Daftar diperbarui otomatis tiap 15 detik." />
      )}
      {tab === "online" && online.data && online.data.data.length > 0 && (
        <Table label="Sesi online">
          <THead>
            <TR>
              <TH>Username</TH>
              <TH>NAS</TH>
              <TH>IP</TH>
              <TH>Mulai</TH>
              <TH>Trafik</TH>
              <TH>Durasi</TH>
              <TH ariaLabel="aksi" align="right" />
            </TR>
          </THead>
          <TBody>
            {online.data.data.map((session, index) => (
              <TR key={sessionKey(session, index)}>
                <TD>
                  <span className="font-semibold font-mono text-[13px]">
                    {session.username ?? "-"}
                  </span>
                </TD>
                <TD muted>{sessionNas(session) || "-"}</TD>
                <TD muted>
                  <span className="font-mono text-[13px]">{sessionIp(session) || "-"}</span>
                </TD>
                <TD muted>{formatDateTime(sessionStarted(session))}</TD>
                <TD muted>{totalTraffic(sessionBytesIn(session), sessionBytesOut(session))}</TD>
                <TD muted>{formatDuration(sessionDuration(session))}</TD>
                <TD align="right">
                  {session.pppoe_account_id ? (
                    <Button
                      variant="danger"
                      size="sm"
                      disabled={disconnect.isPending}
                      onClick={() => {
                        const accountId = session.pppoe_account_id;
                        if (accountId) {
                          disconnect.mutate(accountId);
                        }
                      }}
                    >
                      <PhoneOff className="size-4" aria-hidden="true" />
                      Disconnect
                    </Button>
                  ) : (
                    <span className="text-slate-400 text-xs">-</span>
                  )}
                </TD>
              </TR>
            ))}
          </TBody>
        </Table>
      )}

      {tab === "history" && history.data && history.data.data.length === 0 && (
        <EmptyState title="Belum ada riwayat sesi" hint="Ubah saringan bila perlu." />
      )}
      {tab === "history" && history.data && history.data.data.length > 0 && (
        <>
          <Table label="Riwayat sesi">
            <THead>
              <TR>
                <TH>Username</TH>
                <TH>Mulai</TH>
                <TH>Selesai</TH>
                <TH>Trafik</TH>
                <TH>Penyebab putus</TH>
              </TR>
            </THead>
            <TBody>
              {history.data.data.map((session, index) => (
                <TR key={sessionKey(session, index)}>
                  <TD>
                    <span className="font-semibold font-mono text-[13px]">
                      {session.username ?? "-"}
                    </span>
                  </TD>
                  <TD muted>{formatDateTime(sessionStarted(session))}</TD>
                  <TD muted>{formatDateTime(sessionStopped(session))}</TD>
                  <TD muted>{totalTraffic(sessionBytesIn(session), sessionBytesOut(session))}</TD>
                  <TD muted>{session.terminate_cause ?? "-"}</TD>
                </TR>
              ))}
            </TBody>
          </Table>
          <Pagination
            page={page}
            perPage={PER_PAGE}
            total={history.data.total}
            onPage={setPage}
          />
        </>
      )}
    </div>
  );
}
