import { useCallback, useEffect, useState } from "react";
import type { FormEvent } from "react";
import { Check, Plus, X } from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import { ApiError } from "../../../shared/lib/api";
import { createOrder, listOrders, setOrderStatus } from "../api";
import type { Order } from "../api";
import { createOrderSchema } from "../api";

export function OrdersPage() {
  const [orders, setOrders] = useState<Order[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [item, setItem] = useState("");
  const [qty, setQty] = useState("1");
  const [formError, setFormError] = useState<string | null>(null);

  const load = useCallback(async (): Promise<void> => {
    setLoading(true);
    setError(null);
    try {
      setOrders(await listOrders());
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to load orders");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  async function onCreate(e: FormEvent): Promise<void> {
    e.preventDefault();
    const parsed = createOrderSchema.safeParse({ item, qty });
    if (!parsed.success) {
      setFormError(parsed.error.issues[0]?.message ?? "invalid input");
      return;
    }
    setFormError(null);
    try {
      await createOrder(parsed.data);
      setItem("");
      setQty("1");
      await load();
    } catch (err) {
      setFormError(err instanceof ApiError ? err.message : "failed to create order");
    }
  }

  async function onStatus(id: string, status: "done" | "cancelled"): Promise<void> {
    setError(null);
    try {
      await setOrderStatus(id, status);
      await load();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to update order");
    }
  }

  return (
    <div>
      <h1>Orders</h1>
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
      <form className="card row" onSubmit={onCreate}>
        <label className="field grow" htmlFor="item">
          Item
          <input
            id="item"
            className="input"
            value={item}
            onChange={(e) => setItem(e.target.value)}
            placeholder="keyboard"
          />
        </label>
        <label className="field" htmlFor="qty">
          Qty
          <input
            id="qty"
            className="input small"
            inputMode="numeric"
            value={qty}
            onChange={(e) => setQty(e.target.value)}
          />
        </label>
        <button type="submit" className="btn inline-flex items-center gap-2">
          <Plus className="size-4" aria-hidden="true" />
          Add order
        </button>
      </form>
      {formError && (
        <p className="error" role="alert">
          {formError}
        </p>
      )}
      {loading ? (
        <p className="muted">Loading…</p>
      ) : orders.length === 0 ? (
        <p className="muted">No orders yet.</p>
      ) : (
        <table className="table">
          <thead>
            <tr>
              <th>Item</th>
              <th>Qty</th>
              <th>Status</th>
              <th>Created</th>
              <th aria-label="actions" />
            </tr>
          </thead>
          <tbody>
            <AnimatePresence initial={false}>
              {orders.map((o) => (
                <motion.tr
                  key={o.id}
                  initial={{ opacity: 0 }}
                  animate={{ opacity: 1 }}
                  exit={{ opacity: 0 }}
                >
                  <td>{o.item}</td>
                  <td>{o.qty}</td>
                  <td>
                    <span className={`badge ${o.status}`}>{o.status}</span>
                  </td>
                  <td className="muted">{new Date(o.createdAt).toLocaleString()}</td>
                  <td className="actions">
                    {o.status === "open" && (
                      <>
                        <button
                          type="button"
                          className="btn btn-small inline-flex items-center gap-1"
                          onClick={() => onStatus(o.id, "done")}
                        >
                          <Check className="size-4" aria-hidden="true" />
                          Done
                        </button>
                        <button
                          type="button"
                          className="btn btn-small btn-secondary inline-flex items-center gap-1"
                          onClick={() => onStatus(o.id, "cancelled")}
                        >
                          <X className="size-4" aria-hidden="true" />
                          Cancel
                        </button>
                      </>
                    )}
                  </td>
                </motion.tr>
              ))}
            </AnimatePresence>
          </tbody>
        </table>
      )}
    </div>
  );
}
