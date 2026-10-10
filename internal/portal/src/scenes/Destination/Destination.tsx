import "./Destination.scss";

import type { JSX } from "react";
import { Link, Route, Routes, useLocation, useParams } from "react-router-dom";
import useSWR from "swr";

import { ApiError, formatError } from "../../app";
import { CopyButton } from "../../common/CopyButton/CopyButton";
import DestinationStatusBadge from "../../common/DestinationStatusBadge/DestinationStatusBadge";
import JSONViewer from "../../common/JSONViewer/JSONViewer";
import { Loading } from "../../common/Icons";
import NotFoundState from "../../common/NotFoundState/NotFoundState";
import CONFIGS, { SHOW_MCP_DESTINATIONS } from "../../config";
import {
  useAllDestinationTypes,
  useDestinationType,
} from "../../destination-types";
import type {
  Destination as DestinationType,
  DestinationTypeReference,
} from "../../typings/Destination";
import {
  isMCPDestinationType,
  lookupType,
  typeLabel,
} from "../../utils/destinationTypes";
import getLogo from "../../utils/logo";
import DestinationMetrics from "./DestinationMetrics";
import DestinationSettings from "./DestinationSettings/DestinationSettings";
import { AttemptRoutes } from "./Events/Attempts";
import {
  MCPDestinationOverview,
  MCPDestinationSettings,
} from "./MCPDestination/MCPDestination";

interface Tab {
  label: string;
  path: string;
}

const tabs: Tab[] = [
  { label: "Overview", path: "" },
  { label: "Settings", path: "/settings" },
  { label: "Event Deliveries", path: "/deliveries" },
];

function isNotFoundError(error: unknown): boolean {
  return error instanceof ApiError && error.status === 404;
}

const Destination = () => {
  const { destination_id } = useParams();
  const location = useLocation();
  const { data: destination, error } = useSWR<DestinationType>(
    `destinations/${destination_id}`,
    { shouldRetryOnError: (err) => !isNotFoundError(err) },
  );
  const allTypes = useAllDestinationTypes();
  // Undefined while loading, and for unknown or hidden types.
  const type = useDestinationType(destination?.type);
  const isMCP =
    !!destination &&
    isMCPDestinationType(
      destination.type,
      lookupType(allTypes, destination.type),
    );

  const logo = getLogo();

  // A 404 wins over stale data: the destination is gone (e.g. disconnected).
  // Hidden MCP destinations look the same, also on direct URLs.
  let fallback: JSX.Element | null = null;
  let breadcrumb = "...";
  if (isNotFoundError(error) || (isMCP && !SHOW_MCP_DESTINATIONS)) {
    breadcrumb = "Not found";
    fallback = (
      <NotFoundState
        title="Destination not found"
        message="This destination doesn't exist or was removed."
      />
    );
  } else if (!destination && error) {
    breadcrumb = "Error";
    fallback = (
      <NotFoundState
        title="Couldn't load destination"
        message={formatError(error)}
      />
    );
  } else if (!destination || !allTypes) {
    fallback = (
      <div className="loading-container">
        <Loading />
      </div>
    );
  } else if (!isMCP && !type) {
    breadcrumb = destination.type;
    fallback = (
      <NotFoundState
        title="Unknown destination type"
        message={`This destination's type, "${destination.type}", can't be displayed here.`}
      />
    );
  } else {
    breadcrumb = typeLabel(destination.type, type);
  }

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
          <span className="subtitle-m">{breadcrumb}</span>
        </div>
      </header>
      {fallback || !destination ? (
        fallback
      ) : (
        <div>
          <div className="header-container">
            <div
              className="header-container__icon"
              dangerouslySetInnerHTML={{ __html: type?.icon ?? "" }}
            />
            <div className="header-container__content">
              <h1 className="title-3xl">{breadcrumb}</h1>
              <p className="body-m">
                {/* An MCP target is an agent's callback URL: shown, not linked. */}
                {destination.target_url && !isMCP ? (
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
                isMCP ? (
                  <MCPDestinationSettings destination={destination} />
                ) : type ? (
                  <DestinationSettings destination={destination} type={type} />
                ) : null
              }
            />
            <Route
              path="/deliveries/*"
              element={<AttemptRoutes destination={destination} />}
            />
            <Route
              path="/"
              element={
                isMCP ? (
                  <MCPDestinationOverview destination={destination} />
                ) : type ? (
                  <DestinationOverview destination={destination} type={type} />
                ) : null
              }
            />
          </Routes>
        </div>
      )}
    </>
  );
};

export default Destination;

function DestinationOverview({
  destination,
  type,
}: {
  destination: DestinationType;
  type: DestinationTypeReference;
}) {
  return (
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
                  : destination.topics.map((topic) => topic).join(", ")}
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
          {Object.entries(destination.credentials).map(([key, value]) => (
            <DestinationDetailsField
              key={key}
              fieldType="credentials"
              fieldKey={key}
              type={type}
              value={value}
            />
          ))}
          <li>
            <span className="body-m">Created At</span>
            <span className="body-m">
              {new Date(destination.created_at).toLocaleString("en-US", {
                year: "numeric",
                month: "short",
                day: "numeric",
                hour: "numeric",
                minute: "2-digit",
                hour12: true,
              })}
            </span>
          </li>
          <li>
            <span className="body-m">Status</span>
            <span className="body-m">
              <DestinationStatusBadge destination={destination} />
            </span>
          </li>
        </ul>
      </div>
      {CONFIGS.ENABLE_DESTINATION_FILTER === "true" &&
        destination.filter &&
        Object.keys(destination.filter).length > 0 && (
          <div className="filter-container">
            <JSONViewer data={destination.filter} label="Event Filter" />
          </div>
        )}
      <DestinationMetrics destination={destination} />
    </>
  );
}

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
