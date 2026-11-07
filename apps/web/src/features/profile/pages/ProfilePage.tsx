import { useState } from "react";
import type { ChangeEvent } from "react";
import { ApiError } from "../../../shared/lib/api";
import { useAuth } from "../../auth/AuthProvider";
import { uploadAvatar } from "../api";

export function ProfilePage() {
  const auth = useAuth();
  const [preview, setPreview] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);

  async function onPick(e: ChangeEvent<HTMLInputElement>): Promise<void> {
    const file = e.target.files?.[0];
    if (!file) {
      return;
    }
    setError(null);
    setDone(false);
    setBusy(true);
    try {
      const url = await uploadAvatar(file);
      setPreview(url);
      await auth.refresh();
      setDone(true);
    } catch (err) {
      setError(err instanceof ApiError || err instanceof Error ? err.message : "upload failed");
    } finally {
      setBusy(false);
      e.target.value = "";
    }
  }

  const current = preview ?? auth.user?.avatarUrl ?? "";

  return (
    <div>
      <h1>Profile</h1>
      <div className="card profile">
        {current ? (
          <img className="avatar large" src={current} alt="Your avatar" />
        ) : (
          <div className="avatar large placeholder" aria-hidden="true">
            {(auth.user?.username ?? "?").slice(0, 1).toUpperCase()}
          </div>
        )}
        <div>
          <p>
            <strong>{auth.user?.username}</strong>
          </p>
          <label className="btn btn-secondary" htmlFor="avatar-file">
            {busy ? "Uploading…" : "Change avatar"}
          </label>
          <input
            id="avatar-file"
            type="file"
            accept="image/*"
            className="hidden"
            onChange={onPick}
            disabled={busy}
          />
          <p className="muted">Converted to WebP in your browser before upload.</p>
          {error && (
            <p className="error" role="alert">
              {error}
            </p>
          )}
          {done && !error && <p className="success">Avatar updated.</p>}
        </div>
      </div>
    </div>
  );
}
