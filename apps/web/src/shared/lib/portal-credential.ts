/** Kredensial sekali-tampil dari endpoint portal-account (buat/reset). */

export type PortalCredential = {
  username: string | null;
  password: string | null;
};

export type PortalAccountInput = {
  username?: string | undefined;
  password?: string | undefined;
};

/** Body portal: hanya kirim field terisi (sandi kosong = acak di server). */
export function portalAccountBody(input: PortalAccountInput): Record<string, string> {
  const body: Record<string, string> = {};
  if (input.username !== undefined && input.username !== "") {
    body["username"] = input.username;
  }
  if (input.password !== undefined && input.password !== "") {
    body["password"] = input.password;
  }
  return body;
}

/** Ambil kredensial dari envelope `{data}` (toleran beberapa alias kunci). */
export function extractPortalCredential(raw: unknown): PortalCredential {
  if (typeof raw !== "object" || raw === null) {
    return { username: null, password: null };
  }
  const data: unknown = (raw as { data?: unknown }).data ?? raw;
  if (typeof data !== "object" || data === null) {
    return { username: null, password: null };
  }
  const record = data as Record<string, unknown>;
  const pick = (...keys: string[]): string | null => {
    for (const key of keys) {
      const value: unknown = record[key];
      if (typeof value === "string" && value.length > 0) {
        return value;
      }
    }
    return null;
  };
  return {
    username: pick("username", "portal_username", "login"),
    password: pick("password", "portal_password", "new_password", "temporary_password"),
  };
}
