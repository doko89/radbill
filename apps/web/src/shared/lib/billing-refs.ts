import { z } from "zod";
import { billingRequest, withQuery } from "./billing-api";

/**
 * Minimal reference lists shared by billing features (select options).
 * Lives in `shared/` so features never import each other.
 */

const optionEnvelope = z.object({
  data: z.array(z.object({ id: z.string() }).catchall(z.unknown())),
  total: z.number().optional(),
});

export type BillingRefOption = {
  id: string;
  label: string;
};

function labelOf(item: { id: string } & Record<string, unknown>, keys: string[]): string {
  for (const key of keys) {
    const value: unknown = item[key];
    if (typeof value === "string" && value.length > 0) {
      return value;
    }
  }
  return item.id;
}

async function listOptions(path: string, labelKeys: string[]): Promise<BillingRefOption[]> {
  const raw: unknown = await billingRequest(withQuery(path, { per_page: 200 }));
  const parsed = optionEnvelope.parse(raw);
  return parsed.data.map((item) => ({
    id: item.id,
    label: labelOf(item as { id: string } & Record<string, unknown>, labelKeys),
  }));
}

export function listCustomerOptions(): Promise<BillingRefOption[]> {
  return listOptions("/api/v1/admin/customers", ["nama", "name", "username"]);
}

export function listPackageOptions(): Promise<BillingRefOption[]> {
  return listOptions("/api/v1/admin/packages", ["nama", "name"]);
}

export function listPppoeOptions(): Promise<BillingRefOption[]> {
  return listOptions("/api/v1/admin/pppoe-accounts", ["username", "nama", "name"]);
}

export function listNasOptions(): Promise<BillingRefOption[]> {
  return listOptions("/api/v1/admin/nas", ["nama", "name", "ip", "host"]);
}
