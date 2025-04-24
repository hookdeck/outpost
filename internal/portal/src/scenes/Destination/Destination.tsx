import "./Destination.scss";

import { Link, Route, Routes, useLocation, useParams } from "react-router-dom";
import useSWR from "swr";

import Badge from "../../common/Badge/Badge";
import { CopyButton } from "../../common/CopyButton/CopyButton";
import { Loading } from "../../common/Icons";
import CONFIGS from "../../config";
import { useDestinationType } from "../../destination-types";
import {
  Destination as DestinationType,
  DestinationTypeReference,
} from "../../typings/Destination";
import getLogo from "../../utils/logo";
import DestinationSettings from "./DestinationSettings/DestinationSettings";
import Events, { EventRoutes } from "./Events/Events";

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
  const { data: destination } = useSWR<DestinationType>(
    `destinations/${destination_id}`
  );
  const type = useDestinationType(destination?.type);

  const logo = getLogo();

  return (
    <>
      <header className="layout__header">
        <a href="/">
          {logo ? (
            logo.indexOf("http") === 0 ? (
              <img
                className="layout__header-logo"
                src={logo}
                alt={CONFIGS.ORGANIZATION_NAME}
              />
            ) : (
              <div
                className="layout__header-logo"
                dangerouslySetInnerHTML={{ __html: logo }}
              />
            )
          ) : null}
        </a>
        <div className="layout__header-breadcrumbs">
          <Link to="/" className="subtitle-m">
            Event Destinations
          </Link>{" "}
          <span className="subtitle-m">/</span>
          <span className="subtitle-m">{type?.label || "..."}</span>
        </div>
      </header>
      {!type || !destination ? (
        <div className="loading-container">
          <Loading />
        </div>
      ) : (
        <div>
          <div className="header-container">
            <div
              className="header-container__icon"
              dangerouslySetInnerHTML={{ __html: type.icon as string }}
            />
            <div className="header-container__content">
              <h1 className="title-3xl">{type.label}</h1>
              <p className="body-m">
                {destination.target_url ? (
                  <>
                    <a
                      href={destination.target_url}
                      target="_blank"
                      rel="noreferrer noopener"
                    >
                      {destination.target}{" "}
                    </a>
                    <CopyButton value={destination.target} />
                  </>
                ) : (
                  <>
                    {destination.target}{" "}
                    <CopyButton value={destination.target} />
                  </>
                )}
              </p>
            </div>
          </div>
          <div className="tabs-container">
            <nav className="tabs">
              {tabs.map((tab) => {
                let isActive = false;
                if (tab.path === "") {
                  isActive =
                    location.pathname === `/destinations/${destination_id}`;
                } else {
                  isActive = location.pathname.includes(
                    `/destinations/${destination_id}${tab.path}`
                  );
                }

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
              element={
                <DestinationSettings destination={destination} type={type} />
              }
            />
            <Route
              path="/events/*"
              element={<EventRoutes destination={destination} />}
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
                          {destination.id} <CopyButton value={destination.id} />
                        </span>
                      </li>
                      {CONFIGS.TOPICS && (
                        <li>
                          <span className="body-m">Topics</span>
                          <span className="mono-s">
                            {destination.topics.length === 1 &&
                            destination.topics[0] === "*"
                              ? "All"
                              : destination.topics
                                  .map((topic) => topic)
                                  .join(", ")}
                          </span>
                        </li>
                      )}
                      {Object.entries(destination.config).map(
                        ([key, value]) => (
                          <DestinationDetailsField
                            key={key}
                            fieldType="config"
                            fieldKey={key}
                            type={type}
                            value={value}
                          />
                        )
                      )}
                      {Object.entries(destination.credentials).map(
                        ([key, value]) => (
                          <DestinationDetailsField
                            key={key}
                            fieldType="credentials"
                            fieldKey={key}
                            type={type}
                            value={value}
                          />
                        )
                      )}
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
                            <Badge text="Disabled" />
                          )}
                        </span>
                      </li>
                    </ul>
                  </div>
                  {/* 
                  TODO: Uncomment when metrics are implemented
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
                  </div> */}
                </>
              }
            />
          </Routes>
        </div>
      )}
    </>
  );
};

export default Destination;

function DestinationDetailsField(props: {
  type: DestinationTypeReference;
  fieldType: "config" | "credentials";
  fieldKey: string;
  value: JSX.Element;
}) {
  let label = "";
  if (props.fieldType === "config") {
    label =
      props.type.config_fields.find((field) => field.key === props.fieldKey)
        ?.label || "";
  } else {
    label =
      props.type.credential_fields.find((field) => field.key === props.fieldKey)
        ?.label || "";
  }
  if (label === "") {
    label = props.fieldKey
      .split("_")
      .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
      .join(" ");
  }

  if (!props.value) {
    return null;
  }

  return (
    <li>
      <span className="body-m">{label}</span>
      <span className="mono-s">{props.value}</span>
    </li>
  );
}
