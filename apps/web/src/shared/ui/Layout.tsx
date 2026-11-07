import { Boxes, LogOut } from "lucide-react";
import { Link, Outlet, useNavigate } from "react-router-dom";
import { useAuth } from "../../features/auth/AuthProvider";

export function Layout() {
  const auth = useAuth();
  const navigate = useNavigate();

  async function onLogout(): Promise<void> {
    await auth.logout();
    navigate("/login");
  }

  return (
    <div className="container">
      <header className="nav">
        <Link to="/" className="brand inline-flex items-center gap-2">
          <Boxes className="size-5" aria-hidden="true" />
          Boilerplate
        </Link>
        <div className="nav-right">
          <Link to="/profile" className="profile-link" aria-label="Profile">
            {auth.user?.avatarUrl ? (
              <img className="avatar" src={auth.user.avatarUrl} alt="" />
            ) : (
              <span className="avatar placeholder" aria-hidden="true">
                {(auth.user?.username ?? "?").slice(0, 1).toUpperCase()}
              </span>
            )}
            <span className="muted">{auth.user?.username}</span>
          </Link>
          <button
            type="button"
            className="btn btn-secondary inline-flex items-center gap-2"
            onClick={onLogout}
          >
            <LogOut className="size-4" aria-hidden="true" />
            Logout
          </button>
        </div>
      </header>
      <main>
        <Outlet />
      </main>
    </div>
  );
}
