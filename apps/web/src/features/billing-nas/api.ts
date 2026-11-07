import { z } from "zod";
import { billingRequest, billingUpdate, withQuery } from "../../shared/lib/billing-api";
import { firstText } from "../../shared/lib/billing-format";

export const nasSchema = z.object({
  id: z.string(),
  nama: z.string().nullish(),
  name: z.string().nullish(),
  ip: z.string().nullish(),
  host: z.string().nullish(),
  address: z.string().nullish(),
  coa_port: z.number().nullish(),
  deskripsi: z.string().nullish(),
  description: z.string().nullish(),
  created_at: z.string().nullish(),
  updated_at: z.string().nullish(),
});
export type Nas = z.infer<typeof nasSchema>;

const nasListEnvelope = z.object({ data: z.array(nasSchema), total: z.number() });
const nasItemEnvelope = z.object({ data: nasSchema });

export type NasListParams = {
  search?: string | undefined;
  page?: number | undefined;
  per_page?: number | undefined;
};

export type NasList = {
  data: Nas[];
  total: number;
};

export async function listNas(params: NasListParams = {}): Promise<NasList> {
  const raw: unknown = await billingRequest(
    withQuery("/api/v1/admin/nas", { ...params, q: params.search ?? null }),
  );
  return nasListEnvelope.parse(raw);
}

export async function getNas(id: string): Promise<Nas> {
  const raw: unknown = await billingRequest(`/api/v1/admin/nas/${id}`);
  return nasItemEnvelope.parse(raw).data;
}

export type NasFormInput = {
  nama: string;
  ip: string;
  secret?: string | undefined;
  coa_port?: number | undefined;
};

function nasBody(input: NasFormInput): Record<string, string | number> {
  const body: Record<string, string | number> = { nama: input.nama, name: input.nama, ip: input.ip };
  if (input.secret) {
    body["secret"] = input.secret;
  }
  if (input.coa_port !== undefined) {
    body["coa_port"] = input.coa_port;
  }
  return body;
}

export async function createNas(input: NasFormInput): Promise<Nas> {
  const raw: unknown = await billingRequest("/api/v1/admin/nas", {
    method: "POST",
    body: nasBody(input),
  });
  return nasItemEnvelope.parse(raw).data;
}

export async function updateNas(id: string, input: NasFormInput): Promise<Nas> {
  const raw: unknown = await billingUpdate(`/api/v1/admin/nas/${id}`, nasBody(input));
  return nasItemEnvelope.parse(raw).data;
}

export async function deleteNas(id: string): Promise<void> {
  await billingRequest<unknown>(`/api/v1/admin/nas/${id}`, { method: "DELETE" });
}

export type CoaTestResult = {
  ok: boolean;
  message: string;
};

function extractCoaResult(raw: unknown): CoaTestResult {
  let data: unknown = raw;
  if (typeof raw === "object" && raw !== null && "data" in raw) {
    data = (raw as { data?: unknown }).data;
  }
  if (typeof data === "object" && data !== null) {
    const record = data as Record<string, unknown>;
    const messageValue = record["message"] ?? record["detail"] ?? record["result"];
    const message =
      typeof messageValue === "string" && messageValue.length > 0
        ? messageValue
        : "Tes CoA berhasil.";
    const flag = record["ok"] ?? record["success"];
    if (flag === false) {
      return { ok: false, message };
    }
    return { ok: true, message };
  }
  if (typeof data === "string" && data.length > 0) {
    return { ok: true, message: data };
  }
  return { ok: true, message: "Tes CoA berhasil." };
}

export async function testNasCoa(id: string): Promise<CoaTestResult> {
  const raw: unknown = await billingRequest(`/api/v1/admin/nas/${id}/test-coa`, {
    method: "POST",
    body: {},
  });
  return extractCoaResult(raw);
}

export const nasFormSchema = z.object({
  nama: z.string().min(1, "nama wajib diisi").max(120),
  ip: z.string().min(1, "IP wajib diisi").max(64),
});

export function nasName(nas: Nas): string {
  return firstText(nas.nama, nas.name, nas.id);
}

export function nasIp(nas: Nas): string {
  return firstText(nas.ip, nas.host, nas.address);
}
