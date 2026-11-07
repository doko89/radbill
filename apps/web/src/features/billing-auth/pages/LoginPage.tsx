import { useState } from "react";
import type { FormEvent } from "react";
import { Navigate, useLocation, useSearchParams } from "react-router-dom";
import { Gauge } from "lucide-react";
import { ApiError } from "../../../shared/lib/api";
import { zodFieldErrors } from "../../../shared/lib/billing-forms";
import { loginRedirectTarget } from "../../../shared/lib/login-redirect";
import {
  Button,
  Card,
  Field,
  FormAlert,
  FormNote,
  Input,
  fieldError,
} from "../../../shared/ui/billing-ui";
import { useBillingAuth } from "../BillingAuthProvider";
import {
  billingLoginSchema,
  billingPasswordReset,
  billingPasswordResetRequest,
  resetConfirmSchema,
  resetRequestSchema,
} from "../api";

type Mode = "login" | "request" | "reset";

/**
 * Satu-satunya halaman login: admin dan pelanggan masuk lewat sini.
 * Tujuan setelah masuk ditentukan peran (admin → /admin, user → /portal).
 * Query `?reset=1` membuka mode minta token (dipakai halaman profil portal).
 */
export function LoginPage() {
  const auth = useBillingAuth();
  const location = useLocation();
  const [params] = useSearchParams();
  const [mode, setMode] = useState<Mode>(params.get("reset") === "1" ? "request" : "login");

  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [token, setToken] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [clientErrors, setClientErrors] = useState<Record<string, string>>({});
  const [serverError, setServerError] = useState<string | null>(null);
  const [serverFields, setServerFields] = useState<Record<string, string> | undefined>(undefined);
  const [note, setNote] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  if (auth.loading) {
    return <p className="muted">Memuat…</p>;
  }
  if (auth.user) {
    return <Navigate to={loginRedirectTarget(auth.user.role, location.state)} replace />;
  }

  function switchMode(next: Mode): void {
    setMode(next);
    setClientErrors({});
    setServerError(null);
    setServerFields(undefined);
    setNote(null);
  }

  function fail(err: unknown): void {
    if (err instanceof ApiError) {
      setServerError(err.message);
      setServerFields(err.fields);
    } else {
      setServerError("permintaan gagal");
      setServerFields(undefined);
    }
  }

  async function onLogin(event: FormEvent): Promise<void> {
    event.preventDefault();
    const parsed = billingLoginSchema.safeParse({ username, password });
    if (!parsed.success) {
      setClientErrors(zodFieldErrors(parsed.error));
      return;
    }
    setClientErrors({});
    setServerError(null);
    setServerFields(undefined);
    setBusy(true);
    try {
      await auth.login(parsed.data);
    } catch (err) {
      fail(err);
    } finally {
      setBusy(false);
    }
  }

  async function onRequestReset(event: FormEvent): Promise<void> {
    event.preventDefault();
    const parsed = resetRequestSchema.safeParse({ username });
    if (!parsed.success) {
      setClientErrors(zodFieldErrors(parsed.error));
      return;
    }
    setClientErrors({});
    setServerError(null);
    setBusy(true);
    try {
      await billingPasswordResetRequest(parsed.data);
      setNote("Jika nama pengguna terdaftar, token reset telah dibuat. Masukkan token di bawah.");
      setMode("reset");
    } catch (err) {
      fail(err);
    } finally {
      setBusy(false);
    }
  }

  async function onConfirmReset(event: FormEvent): Promise<void> {
    event.preventDefault();
    const parsed = resetConfirmSchema.safeParse({
      token,
      new_password: newPassword,
      confirm_password: confirmPassword,
    });
    if (!parsed.success) {
      setClientErrors(zodFieldErrors(parsed.error));
      return;
    }
    setClientErrors({});
    setServerError(null);
    setBusy(true);
    try {
      await billingPasswordReset(parsed.data);
      setNote("Kata sandi berhasil diubah. Silakan masuk.");
      setMode("login");
      setPassword("");
    } catch (err) {
      fail(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="min-h-dvh bg-slate-100 flex items-center justify-center px-4 py-10">
      <div className="w-full max-w-md">
        <div className="flex items-center justify-center gap-2 mb-5">
          <Gauge className="size-6 text-emerald-700" aria-hidden="true" />
          <p className="m-0 text-xl font-bold text-slate-900">RadBill</p>
        </div>
        <Card>
          <h1 className="m-0 text-lg font-bold text-slate-900">
            {mode === "login" ? "Masuk" : mode === "request" ? "Minta reset sandi" : "Atur sandi baru"}
          </h1>
          <p className="text-sm text-slate-500 mt-1 mb-5">
            {mode === "login"
              ? "Gunakan akun admin atau pelanggan Anda."
              : mode === "request"
                ? "Kami siapkan token reset sekali pakai."
                : "Masukkan token dan sandi baru Anda."}
          </p>
          {mode === "login" && (
            <form onSubmit={onLogin} className="flex flex-col gap-4" noValidate>
              <Field
                label="Nama pengguna"
                htmlFor="login-username"
                required
                error={clientErrors["username"] ?? fieldError(serverFields, "username")}
              >
                <Input
                  id="login-username"
                  autoComplete="username"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  invalid={clientErrors["username"] !== undefined || serverFields?.["username"] !== undefined}
                />
              </Field>
              <Field
                label="Kata sandi"
                htmlFor="login-password"
                required
                error={clientErrors["password"] ?? fieldError(serverFields, "password")}
              >
                <Input
                  id="login-password"
                  type="password"
                  autoComplete="current-password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  invalid={clientErrors["password"] !== undefined || serverFields?.["password"] !== undefined}
                />
              </Field>
              {serverError && <FormAlert message={serverError} />}
              {note && <FormNote message={note} />}
              <Button type="submit" disabled={busy}>
                {busy ? "Masuk…" : "Masuk"}
              </Button>
              <p className="m-0 text-sm text-center">
                <button
                  type="button"
                  onClick={() => switchMode("request")}
                  className="text-emerald-700 font-semibold hover:underline cursor-pointer bg-transparent border-0 p-0"
                >
                  Lupa kata sandi?
                </button>
              </p>
            </form>
          )}
          {mode === "request" && (
            <form onSubmit={onRequestReset} className="flex flex-col gap-4" noValidate>
              <Field
                label="Nama pengguna"
                htmlFor="login-reset-username"
                required
                error={clientErrors["username"] ?? fieldError(serverFields, "username")}
              >
                <Input
                  id="login-reset-username"
                  autoComplete="username"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  invalid={clientErrors["username"] !== undefined}
                />
              </Field>
              {serverError && <FormAlert message={serverError} />}
              <Button type="submit" disabled={busy}>
                {busy ? "Memproses…" : "Kirim token reset"}
              </Button>
              <p className="m-0 text-sm text-center">
                <button
                  type="button"
                  onClick={() => switchMode("login")}
                  className="text-emerald-700 font-semibold hover:underline cursor-pointer bg-transparent border-0 p-0"
                >
                  Kembali masuk
                </button>
              </p>
            </form>
          )}
          {mode === "reset" && (
            <form onSubmit={onConfirmReset} className="flex flex-col gap-4" noValidate>
              {note && <FormNote message={note} />}
              <Field
                label="Token reset"
                htmlFor="login-reset-token"
                required
                error={clientErrors["token"] ?? fieldError(serverFields, "token")}
              >
                <Input
                  id="login-reset-token"
                  autoComplete="one-time-code"
                  value={token}
                  onChange={(e) => setToken(e.target.value)}
                  invalid={clientErrors["token"] !== undefined}
                />
              </Field>
              <Field
                label="Sandi baru"
                htmlFor="login-reset-new-password"
                required
                error={
                  clientErrors["new_password"] ?? fieldError(serverFields, "new_password", "password")
                }
              >
                <Input
                  id="login-reset-new-password"
                  type="password"
                  autoComplete="new-password"
                  value={newPassword}
                  onChange={(e) => setNewPassword(e.target.value)}
                  invalid={clientErrors["new_password"] !== undefined}
                />
              </Field>
              <Field
                label="Konfirmasi sandi baru"
                htmlFor="login-reset-confirm-password"
                required
                error={clientErrors["confirm_password"]}
              >
                <Input
                  id="login-reset-confirm-password"
                  type="password"
                  autoComplete="new-password"
                  value={confirmPassword}
                  onChange={(e) => setConfirmPassword(e.target.value)}
                  invalid={clientErrors["confirm_password"] !== undefined}
                />
              </Field>
              {serverError && <FormAlert message={serverError} />}
              <Button type="submit" disabled={busy}>
                {busy ? "Memproses…" : "Ubah kata sandi"}
              </Button>
              <p className="m-0 text-sm text-center">
                <button
                  type="button"
                  onClick={() => switchMode("login")}
                  className="text-emerald-700 font-semibold hover:underline cursor-pointer bg-transparent border-0 p-0"
                >
                  Kembali masuk
                </button>
              </p>
            </form>
          )}
        </Card>
      </div>
    </div>
  );
}
