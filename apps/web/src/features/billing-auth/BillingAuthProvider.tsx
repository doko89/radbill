import { createContext, useContext, useEffect, useState } from "react";
import type { ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { billingLoginRequest, billingLogoutRequest, billingMeRequest } from "./api";
import type { BillingLoginInput, BillingUser } from "./api";

type BillingAuthContextValue = {
  user: BillingUser | null;
  loading: boolean;
  login: (input: BillingLoginInput) => Promise<void>;
  logout: () => Promise<void>;
  refresh: () => Promise<void>;
  hasPermission: (permission: string) => boolean;
};

const BillingAuthContext = createContext<BillingAuthContextValue | null>(null);

export function useBillingAuth(): BillingAuthContextValue {
  const value = useContext(BillingAuthContext);
  if (!value) {
    throw new Error("useBillingAuth must be used inside BillingAuthProvider");
  }
  return value;
}

/**
 * PRD §3: `admin` memegang seluruh `/admin/*`; permission map dipakai untuk
 * role lain (kasir/teknisi di masa depan). `"*"` diterima sebagai superuser.
 */
export function billingGrants(user: BillingUser | null, permission: string): boolean {
  if (!user) {
    return false;
  }
  if (user.role === "admin") {
    return true;
  }
  return user.permissions.includes("*") || user.permissions.includes(permission);
}

/**
 * Token + user billing di memori saja. Bootstrap: GET /auth/me (401 memicu
 * single-flight refresh cookie lalu retry sekali). Harus dipasang di bawah
 * QueryClientProvider karena logout membersihkan cache query billing.
 */
export function BillingAuthProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();
  const [user, setUser] = useState<BillingUser | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let alive = true;
    billingMeRequest()
      .then((u) => {
        if (alive) {
          setUser(u);
        }
      })
      .catch(() => {
        if (alive) {
          setUser(null);
        }
      })
      .finally(() => {
        if (alive) {
          setLoading(false);
        }
      });
    return () => {
      alive = false;
    };
  }, []);

  async function login(input: BillingLoginInput): Promise<void> {
    const u = await billingLoginRequest(input);
    setUser(u);
  }

  async function logout(): Promise<void> {
    try {
      await billingLogoutRequest();
    } finally {
      setUser(null);
      queryClient.clear();
    }
  }

  async function refresh(): Promise<void> {
    try {
      setUser(await billingMeRequest());
    } catch {
      setUser(null);
    }
  }

  function hasPermission(permission: string): boolean {
    return billingGrants(user, permission);
  }

  return (
    <BillingAuthContext.Provider value={{ user, loading, login, logout, refresh, hasPermission }}>
      {children}
    </BillingAuthContext.Provider>
  );
}
