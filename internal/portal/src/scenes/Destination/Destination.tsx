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
import { AttemptRoutes } from "./Events/Attempts";

interface Tab {
  label: string;
  path: string;
}

const tabs: Tab[] = [
  { label: "Overview", path: "" },
  { label: "Settings", path: "/settings" },
  { label: "Attempts", path: "/attempts" },
];

const Destination = () => {
  const { destination_id } = useParams();
  const location = useLocation();
  const { data: destination } = useSWR<DestinationType>(
    `destinations/${destination_id}`,
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
                    `/destinations/${destination_id}${tab.path}`,
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
              path="/attempts/*"
              element={<AttemptRoutes destination={destination} />}
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
                      {Object.entries(destination.config)
                        .filter(([key]) => {
                          // Filter out custom_headers if the feature flag is not enabled
                          if (
                            key === "custom_headers" &&
                            CONFIGS.ENABLE_WEBHOOK_CUSTOM_HEADERS !== "true"
                          ) {
                            return false;
                          }
                          return true;
                        })
                        .map(([key, value]) => (
                          <DestinationDetailsField
                            key={key}
                            fieldType="config"
                            fieldKey={key}
                            type={type}
                            value={value}
                          />
                        ))}
                      {Object.entries(destination.credentials).map(
                        ([key, value]) => (
                          <DestinationDetailsField
                            key={key}
                            fieldType="credentials"
                            fieldKey={key}
                            type={type}
                            value={value}
                          />
                        ),
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
                            },
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
                  {CONFIGS.ENABLE_DESTINATION_FILTER === "true" &&
                    destination.filter &&
                    Object.keys(destination.filter).length > 0 && (
                      <div className="filter-container">
                        <h2 className="title-l">Event Filter</h2>
                        <pre className="filter-json mono-s">
                          {JSON.stringify(destination.filter, null, 2)}
                        </pre>
                      </div>
                    )}
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

const TRUNCATION_LENGTH = 32;

// Fallback safety check: treat values with 3+ consecutive asterisks as obfuscated.
// This catches cases where the API obfuscated the value but metadata doesn't indicate sensitivity.
function looksObfuscated(value: string | JSX.Element): boolean {
  return typeof value === "string" && /\*{3,}/.test(value);
}

function parseKeyValueMap(value: string): [string, string][] | null {
  try {
    const parsed = JSON.parse(value);
    if (typeof parsed === "object" && parsed !== null) {
      const entries = Object.entries(parsed) as [string, string][];
      if (entries.length === 0) {
        return null;
      }
      return entries;
    }
  } catch {
    // Not valid JSON
  }
  return null;
}

function DestinationDetailsField(props: {
  type: DestinationTypeReference;
  fieldType: "config" | "credentials";
  fieldKey: string;
  value: JSX.Element | string;
}) {
  let label = "";
  let isSensitive = false;
  let shouldCopy = false;
  let fieldType: string | undefined;

  if (props.fieldType === "config") {
    const field = props.type.config_fields.find(
      (field) => field.key === props.fieldKey,
    );
    label = field?.label || "";
    fieldType = field?.type;
    shouldCopy = field?.type === "text";
  } else {
    const field = props.type.credential_fields.find(
      (field) => field.key === props.fieldKey,
    );
    label = field?.label || "";
    fieldType = field?.type;

    // Only hide copy button if field is explicitly marked as sensitive in metadata.
    // Fields not in metadata (e.g., webhook.credentials.secret) are auto-generated,
    // and we default to allowing copy since we can't determine sensitivity from metadata.
    const isExplicitlySensitive = field?.sensitive === true;
    const isObfuscated = looksObfuscated(props.value);
    shouldCopy = !isExplicitlySensitive && !isObfuscated;
    isSensitive = isExplicitlySensitive || isObfuscated;
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

  // Render key_value_map fields as a multi-line list
  if (fieldType === "key_value_map" && typeof props.value === "string") {
    const entries = parseKeyValueMap(props.value);
    if (!entries) {
      return null; // Empty map, don't show
    }
    return (
      <li className="key-value-field">
        <span className="body-m">{label}</span>
        <span className="key-value-field__values">
          {entries.map(([k, v]) => (
            <span key={k} className="mono-s key-value-field__entry">
              {k}: {v}
            </span>
          ))}
        </span>
      </li>
    );
  }

  return (
    <li>
      <span className="body-m">{label}</span>
      <span
        className="mono-s"
        title={
          typeof props.value === "string" && !isSensitive
            ? props.value
            : undefined
        }
      >
        {typeof props.value === "string" &&
        props.value.length > TRUNCATION_LENGTH
          ? `${props.value.substring(0, TRUNCATION_LENGTH)}...`
          : props.value}{" "}
        {shouldCopy && <CopyButton value={String(props.value)} />}
      </span>
    </li>
  );
}
