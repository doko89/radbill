import { z } from "zod";
import { api } from "../../shared/lib/api";

export const userSchema = z.object({
  id: z.string(),
  username: z.string(),
  avatarUrl: z.string(),
});
export type User = z.infer<typeof userSchema>;

export const loginSchema = z.object({
  username: z.string().min(1, "username is required").max(64),
  password: z.string().min(1, "password is required").max(128),
});
export type LoginInput = z.infer<typeof loginSchema>;

export const authMethodsSchema = z.object({
  password: z.boolean(),
  oidc: z.object({ enabled: z.boolean(), label: z.string() }),
  ldap: z.object({ enabled: z.boolean(), label: z.string() }),
});
export type AuthMethods = z.infer<typeof authMethodsSchema>;

export type PasswordMethod = "password" | "ldap";

export async function getAuthMethods(): Promise<AuthMethods> {
  const res: unknown = await api("/api/auth/methods");
  return authMethodsSchema.parse(res);
}

export async function loginRequest(input: LoginInput, method: PasswordMethod): Promise<User> {
  const path = method === "ldap" ? "/api/auth/ldap/login" : "/api/auth/login";
  const res: unknown = await api(path, { method: "POST", body: input });
  return userSchema.parse(res);
}

export async function logoutRequest(): Promise<void> {
  await api("/api/auth/logout", { method: "POST" });
}

export async function meRequest(): Promise<User> {
  const res: unknown = await api("/api/auth/me");
  return userSchema.parse(res);
}
