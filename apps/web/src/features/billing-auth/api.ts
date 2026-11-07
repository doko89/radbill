import { z } from "zod";
import { billingRequest, setBillingAccessToken } from "../../shared/lib/billing-api";

/** Billing JWT user (snake_case keys, permissions map per PRD §3). */
export const billingUserSchema = z.object({
  id: z.string(),
  username: z.string(),
  role: z.string(),
  customer_id: z.string().nullable().optional(),
  permissions: z.array(z.string()).default([]),
});
export type BillingUser = z.infer<typeof billingUserSchema>;

export const billingLoginSchema = z.object({
  username: z.string().min(1, "nama pengguna wajib diisi").max(64),
  password: z.string().min(1, "kata sandi wajib diisi").max(256),
});
export type BillingLoginInput = z.infer<typeof billingLoginSchema>;

const loginResponseSchema = z.object({
  data: z.object({
    access_token: z.string(),
    token_type: z.string().nullish(),
    expires_in: z.number().nullish(),
    user: billingUserSchema,
  }),
});

const meResponseSchema = z.object({
  data: z.object({ user: billingUserSchema }),
});

export async function billingLoginRequest(input: BillingLoginInput): Promise<BillingUser> {
  const raw: unknown = await billingRequest("/api/v1/auth/login", {
    method: "POST",
    body: input,
    noAuthRetry: true,
  });
  const parsed = loginResponseSchema.parse(raw);
  setBillingAccessToken(parsed.data.access_token);
  return parsed.data.user;
}

export async function billingMeRequest(): Promise<BillingUser> {
  const raw: unknown = await billingRequest("/api/v1/auth/me");
  return meResponseSchema.parse(raw).data.user;
}

export async function billingLogoutRequest(): Promise<void> {
  try {
    await billingRequest<unknown>("/api/v1/auth/logout", { method: "POST", body: {} });
  } finally {
    setBillingAccessToken(null);
  }
}

export const resetRequestSchema = z.object({
  username: z.string().min(1, "nama pengguna wajib diisi").max(128),
});
export type ResetRequestInput = z.infer<typeof resetRequestSchema>;

export async function billingPasswordResetRequest(input: ResetRequestInput): Promise<void> {
  await billingRequest<unknown>("/api/v1/auth/password/reset-request", {
    method: "POST",
    body: { username: input.username },
    noAuthRetry: true,
  });
}

export const resetConfirmSchema = z
  .object({
    token: z.string().min(1, "token wajib diisi").max(512),
    new_password: z.string().min(8, "kata sandi minimal 8 karakter").max(256),
    confirm_password: z.string().min(1, "konfirmasi wajib diisi").max(256),
  })
  .refine((value) => value.new_password === value.confirm_password, {
    message: "konfirmasi tidak sama",
    path: ["confirm_password"],
  });
export type ResetConfirmInput = z.infer<typeof resetConfirmSchema>;

export async function billingPasswordReset(input: ResetConfirmInput): Promise<void> {
  await billingRequest<unknown>("/api/v1/auth/password/reset", {
    method: "POST",
    body: { token: input.token, new_password: input.new_password, password: input.new_password },
    noAuthRetry: true,
  });
}
