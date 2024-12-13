import { useParams, Link, useLocation, Route, Routes } from "react-router-dom";
import useSWR from "swr";
import { CopyButton } from "../../common/CopyButton/CopyButton";
import { useDestinationType } from "../../destination-types";

import "./Destination.scss";
import Badge from "../../common/Badge/Badge";
import CONFIGS from "../../config";
import DestinationSettings from "./DestinationSettings/DestinationSettings";
import Events from "./Events/Events";

// Define the tab interface
interface Tab {
  label: string;
  path: string;
}

// Define available tabs
const tabs: Tab[] = [
  { label: "Overview", path: "" },
  { label: "Settings", path: "/settings" },
  { label: "Events", path: "/events" },
];

const Destination = () => {
  const { destination_id } = useParams();
  const location = useLocation();
  const { data: destination } = useSWR(`destinations/${destination_id}`);
  const type = useDestinationType(destination?.type);

  if (!destination || !type) return <div>Loading...</div>;

  return (
    <>
      <header className="layout__header">
        <a href="/">
          {CONFIGS.LOGO ? (
            CONFIGS.LOGO.indexOf("http") === 0 ? (
              <img
                className="layout__header-logo"
                src={CONFIGS.LOGO}
                alt={CONFIGS.ORGANIZATION_NAME}
              />
            ) : (
              <div
                className="layout__header-logo"
                dangerouslySetInnerHTML={{ __html: CONFIGS.LOGO }}
              />
            )
          ) : null}
        </a>
        <div className="layout__header-breadcrumbs">
          <Link to="/" className="subtitle-m">
            Event Destinations
          </Link>{" "}
          <span className="subtitle-m">/</span>
          <span className="subtitle-m">{type.label}</span>
        </div>
      </header>
      <div>
        <div className="header-container">
          <div
            className="header-container__icon"
            dangerouslySetInnerHTML={{ __html: type.icon as string }}
          />
          <div className="header-container__content">
            <h1 className="title-3xl">{type.label}</h1>
            <p className="body-m">
              {destination.config[type.target]}{" "}
              <CopyButton value={destination.config[type.target]} />
            </p>
          </div>
        </div>
        <div className="tabs-container">
          <nav className="tabs">
            {tabs.map((tab) => {
              const isActive =
                location.pathname ===
                `/destinations/${destination_id}${tab.path}`;
              return (
                <Link
                  key={tab.path}
                  to={`/destinations/${destination_id}${tab.path}`}
                  className={`tab ${isActive ? "tab--active" : ""}`}
                >
                  {tab.label}
                </Link>
              );
            })}
          </nav>
        </div>

        <Routes>
          <Route
            path="/settings"
            element={<DestinationSettings destination={destination} />}
          />
          <Route
            path="/events"
            element={<Events destination={destination} />}
          />
          <Route
            path="/"
            element={
              <>
                <div className="content-container">
                  <h2 className="title-l">Details</h2>
                  <ul>
                    <li>
                      <span className="body-m">ID</span>
                      <span className="mono-s">
                        {destination.id}{" "}
                        <CopyButton value={destination.config[type.target]} />
                      </span>
                    </li>
                    <li>
                      <span className="body-m">Topics</span>
                      <span className="mono-s">
                        {destination.topics.length === 1 &&
                        destination.topics[0] === "*"
                          ? "All"
                          : destination.topics.map((topic) => topic).join(", ")}
                      </span>
                    </li>
                    {[
                      ...Object.entries(destination.config),
                      ...Object.entries(destination.credentials),
                    ].map(([key, value]) => (
                      <li key={key}>
                        <span className="body-m">
                          {key
                            .split("_")
                            .map(
                              (word) =>
                                word.charAt(0).toUpperCase() + word.slice(1)
                            )
                            .join(" ")}
                        </span>
                        <span className="mono-s">{value}</span>
                      </li>
                    ))}
                    <li>
                      <span className="body-m">Created At</span>
                      <span className="body-m">
                        {new Date(destination.created_at).toLocaleString(
                          "en-US",
                          {
                            year: "numeric",
                            month: "short",
                            day: "numeric",
                            hour: "numeric",
                            minute: "2-digit",
                            hour12: true,
                          }
                        )}
                      </span>
                    </li>
                    <li>
                      <span className="body-m">Status</span>
                      <span className="body-m">
                        {!destination.disabled_at ? (
                          <Badge success text="Active" />
                        ) : (
                          <Badge danger text="Disabled" />
                        )}
                      </span>
                    </li>
                  </ul>
                </div>
                <div className="metrics-container">
                  <h2 className="title-l">Metrics</h2>
                  <div className="metrics-container__metrics">
                    <div className="metrics-container__metric">
                      <div>[TODO]</div>
                    </div>
                    <div className="metrics-container__metric">
                      <div>[TODO]</div>
                    </div>
                  </div>
                </div>
              </>
            }
          />
        </Routes>
      </div>
    </>
  );
};

export default Destination;
