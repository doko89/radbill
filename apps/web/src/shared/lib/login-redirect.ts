/**
 * Satu-satunya penentu tujuan setelah login berhasil.
 *
 * Kontrak: role `admin` → halaman admin, role lain (`user`) → halaman
 * portal pelanggan. State `from` dari guard hanya dihormati bila se-arah
 * dengan peran (mencegah user mendarat di /admin dan sebaliknya) dan
 * bukan halaman login itu sendiri (mencegah loop redirect).
 */
const LOGIN_PATHS: readonly string[] = ["/login", "/admin/login", "/portal/login"];

function safeFrom(state: unknown, prefix: "/admin" | "/portal"): string | null {
  if (typeof state === "object" && state !== null) {
    const from = (state as { from?: unknown }).from;
    if (typeof from === "string" && from.startsWith(prefix) && !LOGIN_PATHS.includes(from)) {
      return from;
    }
  }
  return null;
}

export function loginRedirectTarget(role: string, state: unknown): string {
  if (role === "admin") {
    return safeFrom(state, "/admin") ?? "/admin";
  }
  return safeFrom(state, "/portal") ?? "/portal";
}
