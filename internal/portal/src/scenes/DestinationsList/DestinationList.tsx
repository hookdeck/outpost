import "./DestinationList.scss";

import { useState } from "react";
import useSWR from "swr";

import Badge from "../../common/Badge/Badge";
import Button from "../../common/Button/Button";
import { AddIcon, FilterIcon, Loading } from "../../common/Icons";
import SearchInput from "../../common/SearchInput/SearchInput";
import Table from "../../common/Table/Table";
import Tooltip from "../../common/Tooltip/Tooltip";
import { useDestinationTypes } from "../../destination-types";
import CONFIGS from "../../config";
import { Destination } from "../../typings/Destination";
import Dropdown from "../../common/Dropdown/Dropdown";
import { Checkbox } from "../../common/Checkbox/Checkbox";

const DestinationList: React.FC = () => {
  const { data: destinations } = useSWR<Destination[]>("destinations");
  const destination_types = useDestinationTypes();
  const [searchTerm, setSearchTerm] = useState("");
  const [selectedStatus, setSelectedStatus] = useState<Record<string, boolean>>(
    {}
  );
  const [selectedTopics, setSelectedTopics] = useState<string[]>([]);

  const table_columns = [
    { header: "Type", width: 160 },
    { header: "Target" },
    CONFIGS.TOPICS ? { header: "Topics", width: 120 } : null,
    { header: "Status", width: 120 },
    { header: "Success Rate", width: 120 },
    { header: "Events (24h)", width: 120 },
  ].filter((column) => column !== null);

  const filtered_destinations =
    destination_types && destinations
      ? destinations.filter((destination) => {
          const search_value = searchTerm.toLowerCase();

          if (Object.values(selectedStatus).some((value) => value)) {
            if (selectedStatus.active && selectedStatus.disabled) {
              // Continue to search term filtering
            } else if (selectedStatus.active && destination.disabled_at) {
              return false;
            } else if (selectedStatus.disabled && !destination.disabled_at) {
              return false;
            }
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

          return (
            destination.type.toLowerCase().includes(search_value) ||
            destination.target.toLowerCase().includes(search_value)
          );
        })
      : [];

  const table_rows =
    filtered_destinations?.map((destination) => ({
      id: destination.id,
      entries: [
        <>
          <div
            style={{ minWidth: "16px", width: "16px", display: "flex" }}
            dangerouslySetInnerHTML={{
              __html: destination_types[destination.type].icon as string,
            }}
          />
          <span className="subtitle-m">
            {destination_types[destination.type].label}
          </span>
        </>,
        <span className="muted-variant">{destination.target}</span>,
        CONFIGS.TOPICS ? (
          <Tooltip
            content={
              <div className="destination-list__topics-tooltip">
                {(destination.topics.length > 0 && destination.topics[0] === "*"
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
              {destination.topics.length > 0 && destination.topics[0] === "*"
                ? "All"
                : destination.topics.length}
            </span>
          </Tooltip>
        ) : null,
        destination.disabled_at ? (
          <Badge text="Disabled" />
        ) : (
          <Badge text="Active" success />
        ),
        <span className="muted-variant">99.5% [TODO]</span>, // TODO: Replace with actual success rate data
        <span className="muted-variant">100 [TODO]</span>, // TODO: Replace with actual events count
      ].filter((entry) => entry !== null),
      link: `/destinations/${destination.id}`,
    })) || [];

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
                            : [...prev, topicTrimmed]
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
