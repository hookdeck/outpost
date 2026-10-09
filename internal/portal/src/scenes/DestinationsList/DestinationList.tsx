import "./DestinationList.scss";

import { useMemo, useState } from "react";
import useSWR from "swr";

import Badge from "../../common/Badge/Badge";
import Button from "../../common/Button/Button";
import { Checkbox } from "../../common/Checkbox/Checkbox";
import DestinationStatusBadge from "../../common/DestinationStatusBadge/DestinationStatusBadge";
import Dropdown from "../../common/Dropdown/Dropdown";
import { AddIcon, FilterIcon, Loading } from "../../common/Icons";
import { useBatchedMetrics } from "../../common/MetricsChart/useMetrics";
import SearchInput from "../../common/SearchInput/SearchInput";
import Table from "../../common/Table/Table";
import Tooltip from "../../common/Tooltip/Tooltip";
import CONFIGS, { SHOW_MCP_DESTINATIONS } from "../../config";
import {
  useAllDestinationTypes,
  useDestinationTypes,
} from "../../destination-types";
import type { Destination } from "../../typings/Destination";
import {
  configString,
  destinationStatus,
  isDestinationVisible,
  isMCPDestinationType,
  lookupType,
  typeLabel,
} from "../../utils/destinationTypes";
import getLogo from "../../utils/logo";
import DestinationEventsCell from "./DestinationEventsCell";

const DEFAULT_METRICS_SAMPLE_COUNT = 7;

const DestinationList: React.FC = () => {
  const { data: allDestinations } = useSWR<Destination[]>("destinations");
  const all_destination_types = useAllDestinationTypes();
  const destination_types = useDestinationTypes();

  // Hidden MCP destinations are dropped once, so every count, filter and
  // metrics request below ignores them.
  const destinations = useMemo(
    () =>
      allDestinations && all_destination_types
        ? allDestinations.filter((destination) =>
            isDestinationVisible(
              destination,
              all_destination_types,
              SHOW_MCP_DESTINATIONS,
            ),
          )
        : undefined,
    [allDestinations, all_destination_types],
  );

  const destinationIds = useMemo(
    () => destinations?.map((d) => d.id) ?? [],
    [destinations],
  );
  const { data: batchedMetrics, isLoading: metricsLoading } = useBatchedMetrics(
    {
      measures: ["successful_count", "failed_count"],
      destinationIds,
      timeframe: "24h",
      granularity: "4h",
      filters: {},
    },
  );
  const metricsSampleCount =
    Object.values(batchedMetrics ?? {}).find((points) => points.length > 0)
      ?.length ?? DEFAULT_METRICS_SAMPLE_COUNT;

  const [searchTerm, setSearchTerm] = useState("");
  const [selectedStatus, setSelectedStatus] = useState<Record<string, boolean>>(
    {},
  );
  const [selectedTopics, setSelectedTopics] = useState<string[]>([]);

  const table_columns = [
    { header: "Type", width: 160 },
    { header: "Target" },
    CONFIGS.TOPICS ? { header: "Topics", width: 120 } : null,
    { header: "Status", width: 120 },
    { header: "Event Deliveries 24h", width: 170 },
  ].filter((column) => column !== null);

  // The agent an MCP destination delivers to, shown next to its target.
  const mcpPrincipal = (destination: Destination) =>
    isMCPDestinationType(
      destination.type,
      lookupType(all_destination_types, destination.type),
    )
      ? configString(destination, "principal")
      : undefined;

  const now = Date.now();
  const hasExpiringDestinations =
    destinations?.some((destination) => !!destination.expires_at) ?? false;

  const filtered_destinations =
    destination_types && destinations
      ? destinations.filter((destination) => {
          const search_value = searchTerm.toLowerCase();

          if (
            Object.values(selectedStatus).some((value) => value) &&
            !selectedStatus[destinationStatus(destination, now)]
          ) {
            return false;
          }

          if (selectedTopics.length > 0) {
            const destinationTopics =
              destination.topics[0] === "*"
                ? CONFIGS.TOPICS.split(",")
                : destination.topics;
            if (
              !selectedTopics.some((topic) => destinationTopics.includes(topic))
            ) {
              return false;
            }
          }

          return [
            destination.type,
            destination.target,
            mcpPrincipal(destination),
          ].some((value) => value?.toLowerCase().includes(search_value));
        })
      : [];

  const table_rows = destination_types
    ? filtered_destinations?.map((destination) => {
        // The type may be missing: an unknown type, or mcp once the operator
        // has no MCP-enabled topic left. The row still renders.
        const type = lookupType(destination_types, destination.type);
        const principal = mcpPrincipal(destination);
        return {
          id: destination.id,
          entries: [
            <>
              <div
                style={{ minWidth: "16px", width: "16px", display: "flex" }}
                dangerouslySetInnerHTML={{
                  __html: type?.icon ?? "",
                }}
              />
              <span className="subtitle-m">
                {typeLabel(destination.type, type)}
              </span>
            </>,
            principal ? (
              <span className="destination-list__target">
                <span>{principal}</span>
                <span className="muted-variant">{destination.target}</span>
              </span>
            ) : (
              <span className="muted-variant">{destination.target}</span>
            ),
            CONFIGS.TOPICS ? (
              <Tooltip
                content={
                  <div className="destination-list__topics-tooltip">
                    {(destination.topics.length > 0 &&
                    destination.topics[0] === "*"
                      ? CONFIGS.TOPICS.split(",")
                      : destination.topics
                    )
                      .slice(0, 9)
                      .map((topic) => (
                        <Badge key={topic} text={topic.trim()} />
                      ))}
                    {(destination.topics[0] === "*"
                      ? CONFIGS.TOPICS.split(",").length
                      : destination.topics.length) > 9 && (
                      <span className="subtitle-s muted">
                        +{" "}
                        {(destination.topics[0] === "*"
                          ? CONFIGS.TOPICS.split(",").length
                          : destination.topics.length) - 9}{" "}
                        more
                      </span>
                    )}
                  </div>
                }
              >
                <span className="muted-variant">
                  {destination.topics.length > 0 &&
                  destination.topics[0] === "*"
                    ? "All"
                    : destination.topics.length}
                </span>
              </Tooltip>
            ) : null,
            <DestinationStatusBadge destination={destination} now={now} />,
            <DestinationEventsCell
              metricsData={
                batchedMetrics
                  ? (batchedMetrics[destination.id] ?? [])
                  : undefined
              }
              isLoading={metricsLoading}
              sampleCount={metricsSampleCount}
            />,
          ].filter((entry) => entry !== null),
          link: `/destinations/${destination.id}`,
        };
      }) || []
    : [];

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
        <a href={CONFIGS.REFERER_URL} className="subtitle-m">
          Back to {CONFIGS.ORGANIZATION_NAME} →
        </a>
      </header>
      {destinations && destination_types ? (
        <div className="destination-list">
          <div className="destination-list__header">
            <span className="subtitle-s muted">&nbsp;</span>
            <h1 className="title-3xl">Event Destinations</h1>
            <div className="destination-list__actions">
              <SearchInput
                value={searchTerm}
                onChange={setSearchTerm}
                placeholder="Filter by type or target"
              />
              <Dropdown
                trigger="Status"
                trigger_icon={<FilterIcon />}
                badge_count={
                  Object.values(selectedStatus).filter((v) => !!v).length
                }
              >
                <div className="dropdown-item">
                  <Checkbox
                    label="Active"
                    checked={selectedStatus.active}
                    onChange={() =>
                      setSelectedStatus({
                        ...selectedStatus,
                        active: !selectedStatus.active,
                      })
                    }
                  />
                </div>
                <div className="dropdown-item">
                  <Checkbox
                    label="Disabled"
                    checked={selectedStatus.disabled}
                    onChange={() =>
                      setSelectedStatus({
                        ...selectedStatus,
                        disabled: !selectedStatus.disabled,
                      })
                    }
                  />
                </div>
                {(hasExpiringDestinations || selectedStatus.expired) && (
                  <div className="dropdown-item">
                    <Checkbox
                      label="Expired"
                      checked={selectedStatus.expired}
                      onChange={() =>
                        setSelectedStatus({
                          ...selectedStatus,
                          expired: !selectedStatus.expired,
                        })
                      }
                    />
                  </div>
                )}
              </Dropdown>
              <Dropdown
                trigger="Topics"
                trigger_icon={<FilterIcon />}
                badge_count={selectedTopics.length}
              >
                <div className="dropdown-item">
                  <Checkbox
                    label="All Topics"
                    checked={selectedTopics.length === 0}
                    onChange={() => setSelectedTopics([])}
                  />
                </div>
                {CONFIGS.TOPICS.split(",").map((topic) => (
                  <div className="dropdown-item" key={topic}>
                    <Checkbox
                      label={topic.trim()}
                      checked={selectedTopics.includes(topic.trim())}
                      onChange={() => {
                        const topicTrimmed = topic.trim();
                        setSelectedTopics((prev) =>
                          prev.includes(topicTrimmed)
                            ? prev.filter((t) => t !== topicTrimmed)
                            : [...prev, topicTrimmed],
                        );
                      }}
                    />
                  </div>
                ))}
              </Dropdown>
              <Button primary to="/new">
                <AddIcon /> Add Destination
              </Button>
            </div>
          </div>
          {destinations && (
            <>
              {destinations.length === 0 ? (
                <div className="destination-list__empty-state">
                  <span className="body-m muted">
                    No event destinations yet. Add your first destination to get
                    started.
                  </span>
                </div>
              ) : (
                <Table
                  columns={table_columns}
                  rows={table_rows}
                  footer_label="event destinations"
                />
              )}
            </>
          )}
        </div>
      ) : (
        <Loading />
      )}
    </>
  );
};

export default DestinationList;
