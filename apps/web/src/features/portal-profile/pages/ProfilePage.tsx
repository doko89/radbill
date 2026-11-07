import { useState } from "react";
import type { FormEvent } from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { ApiError } from "../../../shared/lib/api";
import { zodFieldErrors } from "../../../shared/lib/billing-forms";
import { formatDate } from "../../../shared/lib/billing-format";
import {
  Button,
  Card,
  Field,
  FormAlert,
  FormNote,
  Input,
  PageHeader,
  fieldError,
} from "../../../shared/ui/billing-ui";
import { useBillingAuth } from "../../billing-auth/BillingAuthProvider";
import { changePortalPassword, getPortalSummary, portalChangePasswordSchema } from "../api";

export function PortalProfilePage() {
  const auth = useBillingAuth();
  const summary = useQuery({ queryKey: ["portal", "summary"], queryFn: getPortalSummary });

  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [clientErrors, setClientErrors] = useState<Record<string, string>>({});
  const [serverError, setServerError] = useState<string | null>(null);
  const [serverFields, setServerFields] = useState<Record<string, string> | undefined>(undefined);
  const [note, setNote] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function onChangePassword(event: FormEvent): Promise<void> {
    event.preventDefault();
    const parsed = portalChangePasswordSchema.safeParse({
      current_password: currentPassword,
      new_password: newPassword,
      confirm_password: confirmPassword,
    });
    if (!parsed.success) {
      setClientErrors(zodFieldErrors(parsed.error));
      return;
    }
    setClientErrors({});
    setServerError(null);
    setServerFields(undefined);
    setNote(null);
    setBusy(true);
    try {
      await changePortalPassword(parsed.data);
      setNote("Kata sandi berhasil diubah.");
      setCurrentPassword("");
      setNewPassword("");
      setConfirmPassword("");
    } catch (err) {
      if (err instanceof ApiError) {
        setServerError(err.message);
        setServerFields(err.fields);
      } else {
        setServerError("gagal mengubah kata sandi");
        setServerFields(undefined);
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <div>
      <PageHeader title="Profil" description="Data diri dan keamanan akun portal Anda." />
      <div className="flex flex-col gap-5">
        <Card>
          <h2 className="m-0 mb-3 text-base font-bold text-slate-900">Data diri</h2>
          {summary.isPending && (
            <p className="m-0 text-sm text-slate-500" role="status">
              Memuat…
            </p>
          )}
          {summary.isError && (
            <FormAlert
              message={
                summary.error instanceof ApiError ? summary.error.message : "gagal memuat profil"
              }
            />
          )}
          {summary.data && (
            <dl className="m-0 grid gap-2 text-sm sm:grid-cols-[160px_1fr]">
              <dt className="text-slate-500">Nama</dt>
              <dd className="m-0 font-semibold text-slate-900">{summary.data.customer?.name ?? "-"}</dd>
              <dt className="text-slate-500">Telepon</dt>
              <dd className="m-0 font-semibold text-slate-900">{summary.data.customer?.phone ?? "-"}</dd>
              <dt className="text-slate-500">Nama pengguna</dt>
              <dd className="m-0 font-semibold text-slate-900">{auth.user?.username ?? "-"}</dd>
              <dt className="text-slate-500">Akun layanan</dt>
              <dd className="m-0 font-semibold text-slate-900">
                {summary.data.accounts.length === 0
                  ? "-"
                  : summary.data.accounts
                      .map((a) => `${a.username} (${a.package.name}, s.d. ${formatDate(a.expires_at)})`)
                      .join("; ")}
              </dd>
            </dl>
          )}
        </Card>

        <Card>
          <h2 className="m-0 text-base font-bold text-slate-900">Ganti kata sandi</h2>
          <p className="text-sm text-slate-500 mt-1 mb-4">
            Minimal 8 karakter. Sesi di perangkat lain ikut diakhiri.
          </p>
          <form onSubmit={onChangePassword} className="flex flex-col gap-4" noValidate>
            <Field
              label="Kata sandi lama"
              htmlFor="portal-current-password"
              required
              error={clientErrors["current_password"] ?? fieldError(serverFields, "current_password")}
            >
              <Input
                id="portal-current-password"
                type="password"
                autoComplete="current-password"
                value={currentPassword}
                onChange={(e) => setCurrentPassword(e.target.value)}
                invalid={clientErrors["current_password"] !== undefined}
              />
            </Field>
            <Field
              label="Kata sandi baru"
              htmlFor="portal-new-password"
              required
              error={clientErrors["new_password"] ?? fieldError(serverFields, "new_password")}
            >
              <Input
                id="portal-new-password"
                type="password"
                autoComplete="new-password"
                value={newPassword}
                onChange={(e) => setNewPassword(e.target.value)}
                invalid={clientErrors["new_password"] !== undefined}
              />
            </Field>
            <Field
              label="Konfirmasi sandi baru"
              htmlFor="portal-confirm-password"
              required
              error={clientErrors["confirm_password"]}
            >
              <Input
                id="portal-confirm-password"
                type="password"
                autoComplete="new-password"
                value={confirmPassword}
                onChange={(e) => setConfirmPassword(e.target.value)}
                invalid={clientErrors["confirm_password"] !== undefined}
              />
            </Field>
            {serverError && <FormAlert message={serverError} />}
            {note && <FormNote message={note} />}
            <div className="flex flex-wrap items-center gap-3">
              <Button type="submit" disabled={busy}>
                {busy ? "Memproses…" : "Ubah kata sandi"}
              </Button>
              <Link
                to="/login?reset=1"
                className="text-sm text-emerald-700 font-semibold hover:underline"
              >
                Lupa kata sandi?
              </Link>
            </div>
          </form>
        </Card>
      </div>
    </div>
  );
}
