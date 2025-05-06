import { useEffect, useState, createContext } from "react";
import { BrowserRouter, Routes, Route } from "react-router-dom";
import DestinationList from "./scenes/DestinationsList/DestinationList";
import { SWRConfig } from "swr";

import "./global.scss";
import "./app.scss";
import { Loading } from "./common/Icons";
import ErrorBoundary from "./common/ErrorBoundary/ErrorBoundary";
import CONFIGS from "./config";
import Destination from "./scenes/Destination/Destination";
import { ToastProvider } from "./common/Toast/Toast";
import CreateDestination from "./scenes/CreateDestination/CreateDestination";

type ApiClient = {
  fetch: (path: string, init?: RequestInit) => Promise<any>;
};

export const ApiContext = createContext<ApiClient>({} as ApiClient);

export function App() {
  const token = useToken();
  const tenant = useTenant(token ?? undefined);

  // Create API client with tenant and token
  const apiClient: ApiClient = {
    fetch: (path: string, init?: RequestInit) => {
      return fetch(`/api/v1/${tenant?.id}/${path}`, {
        ...init,
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${token}`,
          ...init?.headers,
        },
      }).then(async (res) => {
        if (!res.ok) {
          let error;
          try {
            const data = await res.json();
            error = new Error(data.message);
          } catch (e) {
            error = new Error(res.statusText);
          }
          throw error;
        }
        return res.json();
      });
    },
  };

  return (
    <BrowserRouter
      future={{
        v7_startTransition: true,
        v7_relativeSplatPath: true,
      }}
    >
      <ToastProvider>
        <div className="layout">
          <ErrorBoundary>
            {tenant ? (
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
                  </Routes>
                </SWRConfig>
              </ApiContext.Provider>
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
      window.location.replace("/");
    }
  }, []);

  if (!token) {
    window.location.replace(CONFIGS.REFERER_URL);
    return;
  }

  return token;
}

type Tenant = {
  id: string;
  created_at: string;
};

function useTenant(token?: string): Tenant | undefined {
  const [tenant, setTenant] = useState<Tenant | undefined>();

  useEffect(() => {
    run();

    async function run() {
      if (!token) {
        return;
      }
      const value = decodeJWT(token);
      if (!value.sub) {
        console.error("Invalid token");
        return;
      }
      const tenantId = value.sub;
      // TODO: Replace to use SWR
      const response = await fetch(`/api/v1/${tenantId}`, {
        headers: {
          Authorization: `Bearer ${token}`,
        },
      });
      if (!response.ok) {
        window.location.replace(CONFIGS.REFERER_URL);
        return;
      }
      const tenant = await response.json();
      setTenant(tenant);
    }
  }, [token]);

  return tenant;
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
        .join("")
    );
    return JSON.parse(jsonPayload);
  } catch (e) {
    console.error(e);
    return {};
  }
}
