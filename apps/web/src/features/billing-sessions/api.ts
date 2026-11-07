import { z } from "zod";
import { billingRequest, withQuery } from "../../shared/lib/billing-api";
import { firstNumber, firstText } from "../../shared/lib/billing-format";

export const sessionSchema = z.object({
  id: z.string().nullish(),
  session_id: z.string().nullish(),
  username: z.string().nullish(),
  pppoe_account_id: z.string().nullish(),
  nas_id: z.string().nullish(),
  nas_name: z.string().nullish(),
  nas_ip: z.string().nullish(),
  ip_address: z.string().nullish(),
  framed_ip_address: z.string().nullish(),
  started_at: z.string().nullish(),
  start_time: z.string().nullish(),
  stopped_at: z.string().nullish(),
  stop_time: z.string().nullish(),
  bytes_in: z.number().nullish(),
  input_octets: z.number().nullish(),
  bytes_out: z.number().nullish(),
  output_octets: z.number().nullish(),
  online_seconds: z.number().nullish(),
  duration_seconds: z.number().nullish(),
  terminate_cause: z.string().nullish(),
  online: z.boolean().nullish(),
});
export type RadiusSession = z.infer<typeof sessionSchema>;

const sessionListEnvelope = z.object({ data: z.array(sessionSchema), total: z.number() });

export type SessionList = {
  data: RadiusSession[];
  total: number;
};

export type SessionHistoryParams = {
  username?: string | undefined;
  pppoe_account_id?: string | undefined;
  page?: number | undefined;
  per_page?: number | undefined;
};

export async function listOnlineSessions(perPage = 100): Promise<SessionList> {
  const raw: unknown = await billingRequest(
    withQuery("/api/v1/admin/sessions", { online: true, per_page: perPage }),
  );
  return sessionListEnvelope.parse(raw);
}

export async function listSessionHistory(params: SessionHistoryParams = {}): Promise<SessionList> {
  const raw: unknown = await billingRequest(
    withQuery("/api/v1/admin/sessions/history", { ...params }),
  );
  return sessionListEnvelope.parse(raw);
}

/** Disconnect via aksi PPPoE (endpoint diduplikasi di sini agar fitur tak saling import). */
export async function disconnectSessionByAccount(pppoeAccountId: string): Promise<void> {
  await billingRequest<unknown>(`/api/v1/admin/pppoe-accounts/${pppoeAccountId}/disconnect`, {
    method: "POST",
    body: {},
  });
}

export function sessionKey(session: RadiusSession, index: number): string {
  return `${session.id ?? session.session_id ?? "sesi"}-${index}`;
}

export function sessionStarted(session: RadiusSession): string {
  return firstText(session.started_at, session.start_time);
}

export function sessionStopped(session: RadiusSession): string {
  return firstText(session.stopped_at, session.stop_time);
}

export function sessionIp(session: RadiusSession): string {
  return firstText(session.ip_address, session.framed_ip_address);
}

export function sessionNas(session: RadiusSession): string {
  return firstText(session.nas_name, session.nas_ip, session.nas_id);
}

export function sessionBytesIn(session: RadiusSession): number | null {
  return firstNumber(session.bytes_in, session.input_octets);
}

export function sessionBytesOut(session: RadiusSession): number | null {
  return firstNumber(session.bytes_out, session.output_octets);
}

export function sessionDuration(session: RadiusSession): number | null {
  return firstNumber(session.online_seconds, session.duration_seconds);
}
