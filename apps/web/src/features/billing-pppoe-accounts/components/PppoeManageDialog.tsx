import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiError } from "../../../shared/lib/api";
import { listPackageOptions } from "../../../shared/lib/billing-refs";
import {
  Button,
  Dialog,
  Field,
  FormAlert,
  FormNote,
  Input,
  Select,
} from "../../../shared/ui/billing-ui";
import {
  changePppoePackage,
  createPortalAccount,
  extendPppoeAccount,
  getPppoeAccount,
  pppoeAccountAction,
  resetPortalAccount,
} from "../api";
import type { PortalCredential, PppoeAccount, PppoeAction } from "../api";

const STATUS_ACTIONS: { action: PppoeAction; label: string; danger: boolean }[] = [
  { action: "activate", label: "Aktifkan", danger: false },
  { action: "isolate", label: "Isolir", danger: true },
  { action: "disable", label: "Nonaktifkan", danger: true },
  { action: "disconnect", label: "Disconnect sesi", danger: true },
  { action: "reset-quota", label: "Reset kuota", danger: false },
];

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof ApiError ? error.message : fallback;
}

export function PppoeManageDialog({
  account,
  onClose,
}: {
  account: PppoeAccount;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const [notice, setNotice] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [extendDays, setExtendDays] = useState("30");
  const [newPackageId, setNewPackageId] = useState("");
  const [revealed, setRevealed] = useState<string | null>(null);
  const [revealError, setRevealError] = useState<string | null>(null);
  const [revealing, setRevealing] = useState(false);
  const [credential, setCredential] = useState<PortalCredential | null>(null);
  const [portalUsername, setPortalUsername] = useState(account.username ?? "");
  const [portalPassword, setPortalPassword] = useState("");

  const packages = useQuery({
    queryKey: ["billing", "ref", "packages"],
    queryFn: listPackageOptions,
  });

  function invalidate(): void {
    void queryClient.invalidateQueries({ queryKey: ["billing", "pppoe-accounts"] });
  }

  const statusMutation = useMutation({
    mutationFn: (action: PppoeAction) => pppoeAccountAction(account.id, action),
    onSuccess: (_data, action) => {
      setNotice(`Aksi "${action}" berhasil.`);
      setActionError(null);
      invalidate();
    },
    onError: (error: unknown) => {
      setActionError(errorMessage(error, "aksi gagal"));
      setNotice(null);
    },
  });

  const extendMutation = useMutation({
    mutationFn: (days: number) => extendPppoeAccount(account.id, days),
    onSuccess: () => {
      setNotice("Masa aktif berhasil diperpanjang.");
      setActionError(null);
      invalidate();
    },
    onError: (error: unknown) => {
      setActionError(errorMessage(error, "perpanjangan gagal"));
      setNotice(null);
    },
  });

  const packageMutation = useMutation({
    mutationFn: (packageId: string) => changePppoePackage(account.id, packageId),
    onSuccess: () => {
      setNotice("Paket berhasil diganti.");
      setActionError(null);
      invalidate();
    },
    onError: (error: unknown) => {
      setActionError(errorMessage(error, "ganti paket gagal"));
      setNotice(null);
    },
  });

  const portalMutation = useMutation({
    mutationFn: (input: { mode: "create" | "reset"; username: string; password: string }) =>
      input.mode === "create"
        ? createPortalAccount(account.id, { username: input.username, password: input.password })
        : resetPortalAccount(account.id, { password: input.password }),
    onSuccess: (data) => {
      setCredential(data);
      setPortalPassword("");
      setNotice("Akun portal berhasil diproses. Salin kredensial sekali ini.");
      setActionError(null);
    },
    onError: (error: unknown) => {
      setActionError(errorMessage(error, "akun portal gagal"));
      setNotice(null);
    },
  });

  function onPortal(mode: "create" | "reset"): void {
    const username = portalUsername.trim();
    if (mode === "create" && username === "") {
      setActionError("username portal wajib diisi");
      setNotice(null);
      return;
    }
    if (portalPassword !== "" && portalPassword.length < 8) {
      setActionError("sandi portal minimal 8 karakter (kosongkan untuk acak)");
      setNotice(null);
      return;
    }
    portalMutation.mutate({ mode, username, password: portalPassword });
  }

  const busy =
    statusMutation.isPending ||
    extendMutation.isPending ||
    packageMutation.isPending ||
    portalMutation.isPending;

  async function onReveal(): Promise<void> {
    setRevealing(true);
    setRevealError(null);
    try {
      const detail = await getPppoeAccount(account.id, true);
      if (detail.password) {
        setRevealed(detail.password);
      } else {
        setRevealError("Server tidak mengembalikan sandi.");
      }
    } catch (error) {
      setRevealError(errorMessage(error, "gagal menampilkan sandi"));
    } finally {
      setRevealing(false);
    }
  }

  function onExtend(): void {
    const days = Number(extendDays.trim());
    if (!Number.isInteger(days) || days <= 0) {
      setActionError("jumlah hari harus bilangan bulat positif");
      setNotice(null);
      return;
    }
    extendMutation.mutate(days);
  }

  function onChangePackage(): void {
    if (newPackageId === "") {
      setActionError("pilih paket tujuan dahulu");
      setNotice(null);
      return;
    }
    packageMutation.mutate(newPackageId);
  }

  return (
    <Dialog
      open
      onClose={onClose}
      wide
      title={`Kelola ${account.username ?? account.id}`}
      description="Aksi status, perpanjangan, ganti paket, sandi, dan akun portal."
    >
      <div className="flex flex-col gap-6">
        <section aria-labelledby="pppoe-status-actions">
          <h3 id="pppoe-status-actions" className="m-0 mb-2 text-sm font-bold text-slate-800">
            Aksi status & sesi
          </h3>
          <div className="flex flex-wrap gap-2">
            {STATUS_ACTIONS.map((item) => (
              <Button
                key={item.action}
                variant={item.danger ? "danger" : "secondary"}
                size="sm"
                disabled={busy}
                onClick={() => statusMutation.mutate(item.action)}
              >
                {statusMutation.isPending &&
                statusMutation.variables === item.action
                  ? "Memproses…"
                  : item.label}
              </Button>
            ))}
          </div>
        </section>

        <section aria-labelledby="pppoe-extend" className="flex flex-col gap-2">
          <h3 id="pppoe-extend" className="m-0 text-sm font-bold text-slate-800">
            Perpanjang masa aktif
          </h3>
          <div className="flex gap-2">
            <label htmlFor="pppoe-extend-days" className="sr-only">
              Jumlah hari perpanjangan
            </label>
            <Input
              id="pppoe-extend-days"
              inputMode="numeric"
              value={extendDays}
              onChange={(e) => setExtendDays(e.target.value)}
              className="max-w-32"
            />
            <Button variant="secondary" size="sm" disabled={busy} onClick={onExtend}>
              {extendMutation.isPending ? "Memproses…" : "Perpanjang"}
            </Button>
          </div>
        </section>

        <section aria-labelledby="pppoe-change-package" className="flex flex-col gap-2">
          <h3 id="pppoe-change-package" className="m-0 text-sm font-bold text-slate-800">
            Ganti paket
          </h3>
          <div className="flex gap-2">
            <label htmlFor="pppoe-new-package" className="sr-only">
              Paket tujuan
            </label>
            <Select
              id="pppoe-new-package"
              value={newPackageId}
              onChange={(e) => setNewPackageId(e.target.value)}
              disabled={packages.isPending}
            >
              <option value="">Pilih paket tujuan…</option>
              {(packages.data ?? []).map((option) => (
                <option key={option.id} value={option.id}>
                  {option.label}
                </option>
              ))}
            </Select>
            <Button variant="secondary" size="sm" disabled={busy} onClick={onChangePackage}>
              {packageMutation.isPending ? "Memproses…" : "Ganti"}
            </Button>
          </div>
        </section>

        <section aria-labelledby="pppoe-password">
          <h3 id="pppoe-password" className="m-0 mb-2 text-sm font-bold text-slate-800">
            Kata sandi PPPoE
          </h3>
          {revealed === null ? (
            <div className="flex items-center gap-3">
              <p className="m-0 font-mono text-lg tracking-widest" aria-label="Sandi tersembunyi">
                ••••••••
              </p>
              <Button variant="secondary" size="sm" disabled={revealing} onClick={onReveal}>
                {revealing ? "Memuat…" : "Tampilkan sandi"}
              </Button>
            </div>
          ) : (
            <div className="flex flex-col gap-2">
              <Field label="Sandi saat ini" htmlFor="pppoe-revealed">
                <Input id="pppoe-revealed" readOnly value={revealed} className="font-mono" />
              </Field>
              <div>
                <Button variant="secondary" size="sm" onClick={() => setRevealed(null)}>
                  Sembunyikan
                </Button>
              </div>
            </div>
          )}
          {revealError && (
            <div className="mt-2">
              <FormAlert message={revealError} />
            </div>
          )}
          <p className="text-xs text-slate-500 mt-2 mb-0">
            Penampilan sandi memakai <code>?reveal=true</code> eksplisit dan dicatat di audit log.
          </p>
        </section>

        <section aria-labelledby="pppoe-portal">
          <h3 id="pppoe-portal" className="m-0 mb-2 text-sm font-bold text-slate-800">
            Akun login portal
          </h3>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 mb-3">
            <Field
              label="Username portal"
              htmlFor="portal-new-username"
              hint="Wajib saat buat baru."
            >
              <Input
                id="portal-new-username"
                autoComplete="username"
                value={portalUsername}
                onChange={(e) => setPortalUsername(e.target.value)}
              />
            </Field>
            <Field
              label="Sandi portal"
              htmlFor="portal-new-password"
              hint="Min. 8 karakter; kosongkan untuk sandi acak."
            >
              <Input
                id="portal-new-password"
                type="password"
                autoComplete="new-password"
                value={portalPassword}
                onChange={(e) => setPortalPassword(e.target.value)}
              />
            </Field>
          </div>
          <div className="flex flex-wrap gap-2">
            <Button variant="secondary" size="sm" disabled={busy} onClick={() => onPortal("create")}>
              {portalMutation.isPending && portalMutation.variables?.mode === "create"
                ? "Memproses…"
                : "Buat akun portal"}
            </Button>
            <Button variant="secondary" size="sm" disabled={busy} onClick={() => onPortal("reset")}>
              {portalMutation.isPending && portalMutation.variables?.mode === "reset"
                ? "Memproses…"
                : "Reset sandi portal"}
            </Button>
          </div>
          {credential && (
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 mt-3">
              <Field label="Username portal" htmlFor="portal-username">
                <Input id="portal-username" readOnly value={credential.username ?? "-"} />
              </Field>
              <Field label="Sandi portal" htmlFor="portal-password">
                <Input
                  id="portal-password"
                  readOnly
                  value={credential.password ?? "-"}
                  className="font-mono"
                />
              </Field>
            </div>
          )}
        </section>

        {notice && <FormNote message={notice} />}
        {actionError && <FormAlert message={actionError} />}

        <div className="flex justify-end">
          <Button variant="secondary" onClick={onClose}>
            Tutup
          </Button>
        </div>
      </div>
    </Dialog>
  );
}
