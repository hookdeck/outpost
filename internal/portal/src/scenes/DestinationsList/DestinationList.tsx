import "./DestinationList.scss";

import { useState } from "react";
import useSWR from "swr";

import Badge from "../../common/Badge/Badge";
import Button from "../../common/Button/Button";
import { AddIcon, FilterIcon } from "../../common/Icons";
import SearchInput from "../../common/SearchInput/SearchInput";
import Table from "../../common/Table/Table";
import Tooltip from "../../common/Tooltip/Tooltip";
import { useDestinationTypes } from "../../destination-types";
import CONFIGS from "../../config";

// TODO: Add empty state
// TODO: Add loading state
// TODO: Check behavior for large destination counts
// TODO: Fetch destination types from the API instead of hardcoding them
// TODO: Add success rate column
// TODO: Add events count column
// TODO: Add status filter

interface Destination {
  id: string;
  type: "webhooks";
  config: {
    url: string;
  };
  topics: string[];
  disabled_at: string | null;
}

const DestinationList: React.FC = () => {
  const { data: destinations } = useSWR<Destination[]>("destinations");
  const destination_types = useDestinationTypes();
  const [searchTerm, setSearchTerm] = useState("");

  if (Object.keys(destination_types).length === 0) {
    return <div>Loading...</div>;
  }

  const table_columns = [
    { header: "Type", width: 160 },
    { header: "Target" },
    { header: "Topics", width: 120 },
    { header: "Status", width: 120 },
    { header: "Success Rate", width: 120 },
    { header: "Events (24h)", width: 120 },
  ];

  const filtered_destinations = destinations?.filter((destination) => {
    const search_value = searchTerm.toLowerCase();
    return (
      destination.type.toLowerCase().includes(search_value) ||
      destination.config[destination_types[destination.type].target]
        .toLowerCase()
        .includes(search_value) ||
      destination.topics.some((topic) =>
        topic.toLowerCase().includes(search_value)
      )
    );
  });

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
        <span className="muted-variant">
          {destination.config[destination_types[destination.type].target]}
        </span>,
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
        </Tooltip>,
        destination.disabled_at ? (
          <Badge text="Disabled" />
        ) : (
          <Badge text="Active" success />
        ),
        <span className="muted-variant">99.5%</span>, // TODO: Replace with actual success rate data
        <span className="muted-variant">100</span>, // TODO: Replace with actual events count
      ],
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
      <div className="destination-list">
        <div className="destination-list__header">
          <span className="subtitle-s muted">&nbsp;</span>
          <h1 className="title-3xl">Event Destinations</h1>
          <div className="destination-list__actions">
            <SearchInput
              value={searchTerm}
              onChange={setSearchTerm}
              placeholder="Filter by type, target or topic"
            />
            <Button onClick={console.log}>
              <FilterIcon /> Status (TODO)
            </Button>
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
    </>
  );
};

export default DestinationList;
