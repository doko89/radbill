import { createContext, useContext, useEffect, useState } from "react";
import type { ReactNode } from "react";
import { loginRequest, logoutRequest, meRequest } from "./api";
import type { LoginInput, PasswordMethod, User } from "./api";

type AuthContextValue = {
  user: User | null;
  loading: boolean;
  login: (input: LoginInput, method: PasswordMethod) => Promise<void>;
  logout: () => Promise<void>;
  refresh: () => Promise<void>;
};

const AuthContext = createContext<AuthContextValue | null>(null);

export function useAuth(): AuthContextValue {
  const value = useContext(AuthContext);
  if (!value) {
    throw new Error("useAuth must be used inside AuthProvider");
  }
  return value;
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let alive = true;
    meRequest()
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

  async function login(input: LoginInput, method: PasswordMethod): Promise<void> {
    const u = await loginRequest(input, method);
    setUser(u);
  }

  async function logout(): Promise<void> {
    await logoutRequest();
    setUser(null);
  }

  async function refresh(): Promise<void> {
    try {
      setUser(await meRequest());
    } catch {
      setUser(null);
    }
  }

  return <AuthContext.Provider value={{ user, loading, login, logout, refresh }}>{children}</AuthContext.Provider>;
}
