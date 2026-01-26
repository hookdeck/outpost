import { useEffect, useState, createContext, useMemo } from "react";
import { BrowserRouter, Routes, Route, Link } from "react-router-dom";
import DestinationList from "./scenes/DestinationsList/DestinationList";
import useSWR, { SWRConfig } from "swr";

import "./global.scss";
import "./app.scss";
import { Loading } from "./common/Icons";
import ErrorBoundary from "./common/ErrorBoundary/ErrorBoundary";
import CONFIGS from "./config";
import Destination from "./scenes/Destination/Destination";
import { ToastProvider } from "./common/Toast/Toast";
import { SidebarProvider } from "./common/Sidebar/Sidebar";
import CreateDestination from "./scenes/CreateDestination/CreateDestination";

type ApiClient = {
  fetch: (path: string, init?: RequestInit) => Promise<any>;
};

// API error response from the server
export class ApiError extends Error {
  status: number;
  data?: string[];

  constructor(message: string, status: number, data?: string[]) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.data = data;
  }

  // Format error for display - includes validation details if present
  toDisplayString(): string {
    if (this.data && this.data.length > 0) {
      return this.data
        .map((d) => d.charAt(0).toUpperCase() + d.slice(1))
        .join(". ");
    }
    return this.message.charAt(0).toUpperCase() + this.message.slice(1);
  }
}

// Helper to format any error for display
export function formatError(error: unknown): string {
  if (error instanceof ApiError) {
    return error.toDisplayString();
  }
  if (error instanceof Error) {
    return error.message.charAt(0).toUpperCase() + error.message.slice(1);
  }
  return String(error);
}

export const ApiContext = createContext<ApiClient>({} as ApiClient);

type TenantResponse = {
  id: string;
  created_at: string;
};

function NotFound() {
  return (
    <div
      style={{
        textAlign: "center",
        padding: "2rem",
        maxWidth: "500px",
        margin: "0 auto",
      }}
    >
      <h1 style={{ fontSize: "2rem", marginBottom: "1rem", color: "#374151" }}>
        Page Not Found
      </h1>
      <p style={{ fontSize: "1rem", marginBottom: "2rem", color: "#6b7280" }}>
        The page you're looking for doesn't exist.
      </p>
      <Link
        to="/"
        style={{
          color: "#3b82f6",
          textDecoration: "none",
          fontSize: "1rem",
          fontWeight: "500",
        }}
      >
        ← Back to Destinations
      </Link>
    </div>
  );
}

function AuthenticatedApp({
  tenant,
  token,
}: {
  tenant: TenantResponse;
  token: string;
}) {
  const apiClient: ApiClient = {
    fetch: (path: string, init?: RequestInit) => {
      return fetch(`/api/v1/tenants/${tenant.id}/${path}`, {
        ...init,
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${token}`,
          ...init?.headers,
        },
      }).then(async (res) => {
        if (!res.ok) {
          let error: ApiError;
          try {
            const data = await res.json();
            error = new ApiError(
              data.message || res.statusText,
              data.status || res.status,
              Array.isArray(data.data) ? data.data : undefined,
            );
          } catch (e) {
            error = new ApiError(res.statusText, res.status);
          }
          throw error;
        }
        return res.json();
      });
    },
  };

  return (
    <ApiContext.Provider value={apiClient}>
      <SWRConfig
        value={{
          fetcher: (path: string) => apiClient.fetch(path),
        }}
      >
        <Routes>
          <Route path="/" Component={DestinationList} />
          <Route path="/new/*" Component={CreateDestination} />
          <Route
            path="/destinations/:destination_id/*"
            Component={Destination}
          />
          <Route path="*" Component={NotFound} />
        </Routes>
      </SWRConfig>
    </ApiContext.Provider>
  );
}

export function App() {
  const token = useToken();
  const tenant = useTenant(token ?? undefined);

  return (
    <BrowserRouter
      future={{
        v7_startTransition: true,
        v7_relativeSplatPath: true,
      }}
    >
      <ToastProvider>
        <SidebarProvider>
          <div className="layout">
            <ErrorBoundary>
              {tenant && token ? (
                <AuthenticatedApp tenant={tenant} token={token} />
              ) : (
                <div>
                  <Loading />
                </div>
              )}
            </ErrorBoundary>
          </div>
          {!CONFIGS.DISABLE_OUTPOST_BRANDING && (
            <div className="powered-by subtitle-s">
              Powered by{" "}
              <a
                href="https://github.com/hookdeck/outpost"
                target="_blank"
                rel="noreferrer"
              >
                Outpost
              </a>
            </div>
          )}
        </SidebarProvider>
      </ToastProvider>
    </BrowserRouter>
  );
}

function useToken() {
  const [token, setToken] = useState(sessionStorage.getItem("token"));

  useEffect(() => {
    const searchParams = new URLSearchParams(window.location.search);
    const token = searchParams.get("token");
    if (token) {
      setToken(token);
      sessionStorage.setItem("token", token);

      // Preserve the current path from the browser
      const currentPath = window.location.pathname;
      window.location.replace(currentPath);
    }
  }, []);

  if (!token) {
    window.location.replace(CONFIGS.REFERER_URL);
    return;
  }

  return token;
}

function useTenant(token?: string): TenantResponse | undefined {
  const tenantId = useMemo(() => {
    if (!token) return null;
    const value = decodeJWT(token);
    if (!value.sub) {
      console.error("Invalid token");
      return null;
    }
    return value.sub;
  }, [token]);

  const { data } = useSWR<TenantResponse>(
    tenantId && token ? [`/api/v1/tenants/${tenantId}`, token] : null,
    ([url, token]: [string, string]) =>
      fetch(url, {
        headers: { Authorization: `Bearer ${token}` },
      }).then((res) => {
        if (!res.ok) {
          window.location.replace(CONFIGS.REFERER_URL);
          throw new Error("Failed to fetch tenant");
        }
        return res.json();
      }),
    { revalidateOnFocus: false },
  );

  return data;
}

function decodeJWT(token: string) {
  try {
    const base64Url = token.split(".")[1];
    const base64 = base64Url.replace(/-/g, "+").replace(/_/g, "/");
    const jsonPayload = decodeURIComponent(
      atob(base64)
        .split("")
        .map(function (c) {
          return "%" + ("00" + c.charCodeAt(0).toString(16)).slice(-2);
        })
        .join(""),
    );
    return JSON.parse(jsonPayload);
  } catch (e) {
    console.error(e);
    return {};
  }
}
