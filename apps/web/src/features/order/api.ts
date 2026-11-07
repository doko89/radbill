import { z } from "zod";
import { api } from "../../shared/lib/api";

export const orderSchema = z.object({
  id: z.string(),
  item: z.string(),
  qty: z.number(),
  status: z.string(),
  createdAt: z.string(),
});
export type Order = z.infer<typeof orderSchema>;

export const createOrderSchema = z.object({
  item: z.string().min(1, "item is required").max(120),
  qty: z.coerce.number().int().min(1).max(9999),
});
export type CreateOrderInput = z.infer<typeof createOrderSchema>;

export async function listOrders(): Promise<Order[]> {
  const res: unknown = await api("/api/orders/");
  return z.array(orderSchema).parse(res);
}

export async function createOrder(input: CreateOrderInput): Promise<Order> {
  const res: unknown = await api("/api/orders/", { method: "POST", body: input });
  return orderSchema.parse(res);
}

export async function setOrderStatus(id: string, status: "done" | "cancelled"): Promise<Order> {
  const res: unknown = await api(`/api/orders/${id}/status`, { method: "PATCH", body: { status } });
  return orderSchema.parse(res);
}
