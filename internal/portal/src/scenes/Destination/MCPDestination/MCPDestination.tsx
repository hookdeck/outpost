import "../DestinationSettings/DestinationSettings.scss";

import { useContext, useState } from "react";
import { useNavigate } from "react-router-dom";
import { mutate } from "swr";

import { ApiContext, ApiError, formatError } from "../../../app";
import Badge from "../../../common/Badge/Badge";
import Button from "../../../common/Button/Button";
import { CopyButton } from "../../../common/CopyButton/CopyButton";
import DestinationStatusBadge from "../../../common/DestinationStatusBadge/DestinationStatusBadge";
import { DeleteIcon } from "../../../common/Icons";
import JSONViewer from "../../../common/JSONViewer/JSONViewer";
import { showToast } from "../../../common/Toast/Toast";
import { useTopicDeprecations } from "../../../topic-deprecations";
import type { Destination } from "../../../typings/Destination";
import {
  configString,
  mcpDeliveries,
  parseArguments,
} from "../../../utils/destinationTypes";
import { deprecationLabel } from "../../../utils/topics";
import DestinationMetrics from "../DestinationMetrics";

// MCP destinations are subscriptions an agent created through the operator's
// MCP server. The portal shows them read-only: settings, secret rotation,
// enable/disable and filter editing don't apply, and the only action is
// Disconnect, which revokes this one subscription (one event to one callback
// URL), not the agent's other subscriptions.

const TRUNCATION_LENGTH = 48;

function formatDateTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString("en-US", {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
    hour12: true,
  });
}

function TextField({
  label,
  value,
  copy = false,
}: {
  label: string;
  value: string;
  copy?: boolean;
}) {
  return (
    <li>
      <span className="body-m">{label}</span>
      <span className="mono-s" title={value}>
        {value.length > TRUNCATION_LENGTH
          ? `${value.substring(0, TRUNCATION_LENGTH)}...`
          : value}{" "}
        {copy && <CopyButton value={value} />}
      </span>
    </li>
  );
}

export function MCPDestinationOverview({
  destination,
}: {
  destination: Destination;
}) {
  const deprecations = useTopicDeprecations();
  const principal = configString(destination, "principal");
  const event = configString(destination, "event") ?? destination.topics?.[0];
  const url = configString(destination, "url");
  const args = parseArguments(destination.config?.arguments);
  const deprecation = event ? deprecationLabel(deprecations.get(event)) : null;
  // Unlike other types, the filter shows whatever ENABLE_DESTINATION_FILTER
  // says: it is part of what the agent subscribed to, and it isn't editable.
  const hasFilter =
    !!destination.filter && Object.keys(destination.filter).length > 0;

  return (
    <>
      <div className="content-container">
        <h2 className="title-l">Details</h2>
        <ul>
          <TextField label="ID" value={destination.id} copy />
          {principal && <TextField label="Principal" value={principal} copy />}
          {event && (
            <li>
              <span className="body-m">Event</span>
              <span className="mono-s">
                {event}
                {deprecation && <Badge text={deprecation} size="s" />}
              </span>
            </li>
          )}
          {url && <TextField label="Callback URL" value={url} copy />}
          <li>
            <span className="body-m">Expires At</span>
            <span className="body-m">
              {destination.expires_at
                ? formatDateTime(destination.expires_at)
                : "Never"}
            </span>
          </li>
          <li>
            <span className="body-m">Created At</span>
            <span className="body-m">
              {formatDateTime(destination.created_at)}
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
      {args && (
        <div className="filter-container">
          {args.ok ? (
            <JSONViewer data={args.value} label="Arguments" />
          ) : (
            <>
              <h2 className="subtitle-m">Arguments</h2>
              <pre className="filter-json mono-s">{args.raw}</pre>
            </>
          )}
        </div>
      )}
      {hasFilter && (
        <div className="filter-container">
          <JSONViewer data={destination.filter} label="Event Filter" />
        </div>
      )}
      <DestinationMetrics destination={destination} />
    </>
  );
}

export function MCPDestinationSettings({
  destination,
}: {
  destination: Destination;
}) {
  const apiClient = useContext(ApiContext);
  const navigate = useNavigate();
  const [isDisconnecting, setIsDisconnecting] = useState(false);
  const deliveries = mcpDeliveries(destination);

  const handleDisconnect = () => {
    const confirmed = window.confirm(
      `Are you sure you want to disconnect this subscription? Deliveries of ${deliveries} will stop, and the agent will have to subscribe again to receive them.`,
    );
    if (!confirmed) return;

    const done = (message: string) => {
      showToast("success", message);
      // Drop the cached destination so going back shows it as gone.
      mutate(`destinations/${destination.id}`, undefined, {
        revalidate: false,
      });
      mutate("destinations");
      navigate("/");
    };

    setIsDisconnecting(true);
    // The revoke endpoint, not DELETE destinations/:id: it also notifies the
    // agent that its access was revoked.
    apiClient
      .fetch(`mcp/subscriptions/${encodeURIComponent(destination.id)}`, {
        method: "DELETE",
      })
      .then(() => done("Subscription disconnected"))
      .catch((error) => {
        if (error instanceof ApiError && error.status === 404) {
          done("Subscription already disconnected");
          return;
        }
        showToast("error", formatError(error));
      })
      .finally(() => {
        setIsDisconnecting(false);
      });
  };

  return (
    <div className="destination-settings">
      <div className="destination-settings__actions">
        <h2 className="title-l">Disconnect subscription</h2>
        <p className="body-m muted">
          This subscription was created by an agent through an MCP client and
          can't be edited here. Disconnecting it immediately stops deliveries of{" "}
          {deliveries}. The agent's other subscriptions keep receiving events.
        </p>
        <Button onClick={handleDisconnect} loading={isDisconnecting} danger>
          <DeleteIcon />
          Disconnect
        </Button>
      </div>
    </div>
  );
}
