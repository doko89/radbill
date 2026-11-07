import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import { Navigate, useNavigate, useSearchParams } from "react-router-dom";
import { ApiError } from "../../../shared/lib/api";
import { useAuth } from "../AuthProvider";
import { getAuthMethods, loginSchema } from "../api";
import type { AuthMethods, PasswordMethod } from "../api";

export function LoginPage() {
  const auth = useAuth();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const [methods, setMethods] = useState<AuthMethods | null>(null);
  const [method, setMethod] = useState<PasswordMethod>("password");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let alive = true;
    getAuthMethods()
      .then((m) => {
        if (alive) {
          setMethods(m);
        }
      })
      .catch(() => {
        if (alive) {
          setMethods(null);
        }
      });
    return () => {
      alive = false;
    };
  }, []);

  useEffect(() => {
    if (searchParams.get("error") === "oidc_failed") {
      setError("External login failed, please try again.");
    }
  }, [searchParams]);

  if (auth.loading) {
    return <p className="muted">Loading…</p>;
  }
  if (auth.user) {
    return <Navigate to="/" replace />;
  }

  async function onSubmit(e: FormEvent): Promise<void> {
    e.preventDefault();
    const parsed = loginSchema.safeParse({ username, password });
    if (!parsed.success) {
      setError(parsed.error.issues[0]?.message ?? "invalid input");
      return;
    }
    setError(null);
    setBusy(true);
    try {
      await auth.login(parsed.data, method);
      navigate("/");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "login failed");
    } finally {
      setBusy(false);
    }
  }

  const ldapEnabled = methods?.ldap.enabled === true;
  const oidcEnabled = methods?.oidc.enabled === true;

  return (
    <div className="container narrow">
      <h1>Sign in</h1>
      {ldapEnabled && (
        <div className="tabs" role="tablist" aria-label="Login method">
          <button
            type="button"
            role="tab"
            aria-selected={method === "password"}
            className={method === "password" ? "tab active" : "tab"}
            onClick={() => setMethod("password")}
          >
            Password
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={method === "ldap"}
            className={method === "ldap" ? "tab active" : "tab"}
            onClick={() => setMethod("ldap")}
          >
            {methods?.ldap.label ?? "LDAP"}
          </button>
        </div>
      )}
      <form className="card" onSubmit={onSubmit}>
        <label className="field" htmlFor="username">
          Username
          <input
            id="username"
            className="input"
            autoComplete="username"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
          />
        </label>
        <label className="field" htmlFor="password">
          Password
          <input
            id="password"
            className="input"
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </label>
        {error && (
          <p className="error" role="alert">
            {error}
          </p>
        )}
        <button type="submit" className="btn btn-block" disabled={busy}>
          {busy ? "Signing in…" : "Sign in"}
        </button>
        {!ldapEnabled && <p className="muted">Default dev account: admin / admin123</p>}
      </form>
      {oidcEnabled && (
        <>
          <div className="divider" aria-hidden="true">
            <span>or</span>
          </div>
          <a className="btn btn-secondary btn-block" href="/api/auth/oidc/login">
            Sign in with {methods?.oidc.label ?? "SSO"}
          </a>
        </>
      )}
    </div>
  );
}
