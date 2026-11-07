import { z } from "zod";
import { billingRequest, billingUpdate, withQuery } from "../../shared/lib/billing-api";
import { firstText } from "../../shared/lib/billing-format";
import {
  extractPortalCredential,
  portalAccountBody,
} from "../../shared/lib/portal-credential";
import type {
  PortalAccountInput,
  PortalCredential,
} from "../../shared/lib/portal-credential";

export const customerSchema = z.object({
  id: z.string(),
  nama: z.string().nullish(),
  name: z.string().nullish(),
  telp: z.string().nullish(),
  telepon: z.string().nullish(),
  phone: z.string().nullish(),
  alamat: z.string().nullish(),
  address: z.string().nullish(),
  catatan: z.string().nullish(),
  notes: z.string().nullish(),
  account_id: z.string().nullish(),
  created_at: z.string().nullish(),
  updated_at: z.string().nullish(),
});
export type Customer = z.infer<typeof customerSchema>;

const customerListEnvelope = z.object({ data: z.array(customerSchema), total: z.number() });
const customerItemEnvelope = z.object({ data: customerSchema });

export type CustomerListParams = {
  search?: string | undefined;
  page?: number | undefined;
  per_page?: number | undefined;
};

export type CustomerList = {
  data: Customer[];
  total: number;
};

export async function listCustomers(params: CustomerListParams = {}): Promise<CustomerList> {
  const raw: unknown = await billingRequest(
    withQuery("/api/v1/admin/customers", { ...params, q: params.search ?? null }),
  );
  return customerListEnvelope.parse(raw);
}

export async function getCustomer(id: string): Promise<Customer> {
  const raw: unknown = await billingRequest(`/api/v1/admin/customers/${id}`);
  return customerItemEnvelope.parse(raw).data;
}

export type CustomerFormInput = {
  nama: string;
  telp?: string | undefined;
  alamat?: string | undefined;
  catatan?: string | undefined;
};

/** Dual-key body: backend Fase 1 boleh memakai kunci id maupun en. */
function customerBody(input: CustomerFormInput): Record<string, string> {
  const body: Record<string, string> = { nama: input.nama, name: input.nama };
  if (input.telp) {
    body["telp"] = input.telp;
    body["telepon"] = input.telp;
    body["phone"] = input.telp;
  }
  if (input.alamat) {
    body["alamat"] = input.alamat;
    body["address"] = input.alamat;
  }
  if (input.catatan) {
    body["catatan"] = input.catatan;
    body["notes"] = input.catatan;
  }
  return body;
}

export async function createCustomer(input: CustomerFormInput): Promise<Customer> {
  const raw: unknown = await billingRequest("/api/v1/admin/customers", {
    method: "POST",
    body: customerBody(input),
  });
  return customerItemEnvelope.parse(raw).data;
}

export async function updateCustomer(id: string, input: CustomerFormInput): Promise<Customer> {
  const raw: unknown = await billingUpdate(`/api/v1/admin/customers/${id}`, customerBody(input));
  return customerItemEnvelope.parse(raw).data;
}

export async function deleteCustomer(id: string): Promise<void> {
  await billingRequest<unknown>(`/api/v1/admin/customers/${id}`, { method: "DELETE" });
}

export async function createCustomerPortalAccount(
  id: string,
  input: PortalAccountInput,
): Promise<PortalCredential> {
  const raw: unknown = await billingRequest(`/api/v1/admin/customers/${id}/portal-account`, {
    method: "POST",
    body: portalAccountBody(input),
  });
  return extractPortalCredential(raw);
}

export async function resetCustomerPortalAccount(
  id: string,
  input: PortalAccountInput,
): Promise<PortalCredential> {
  const raw: unknown = await billingRequest(`/api/v1/admin/customers/${id}/portal-account/reset`, {
    method: "POST",
    body: portalAccountBody(input),
  });
  return extractPortalCredential(raw);
}

export const customerFormSchema = z.object({
  nama: z.string().min(1, "nama wajib diisi").max(120),
  telp: z.string().max(32).optional(),
  alamat: z.string().max(500).optional(),
  catatan: z.string().max(500).optional(),
});

export function customerName(customer: Customer): string {
  return firstText(customer.nama, customer.name, customer.id);
}

export function customerPhone(customer: Customer): string {
  return firstText(customer.telp, customer.telepon, customer.phone);
}

export function customerAddress(customer: Customer): string {
  return firstText(customer.alamat, customer.address);
}
