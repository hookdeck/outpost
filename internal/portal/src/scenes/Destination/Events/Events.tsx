import { useMemo } from "react";
import Badge from "../../../common/Badge/Badge";
// import Button from "../../../common/Button/Button";
// import SearchInput from "../../../common/SearchInput/SearchInput";
import "./Events.scss";
import Table from "../../../common/Table/Table";
import { EventListResponse } from "../../../typings/Event";
import useSWR from "swr";
import Dropdown from "../../../common/Dropdown/Dropdown";
import { FilterIcon } from "../../../common/Icons";
import { Checkbox } from "../../../common/Checkbox/Checkbox";
import { useSearchParams } from "react-router-dom";

const Events = ({ destination }: { destination: any }) => {
  const { status, urlSearchParams } = useEventFilter();

  const { data: eventsList } = useSWR<EventListResponse>(
    `destinations/${destination.id}/events?${urlSearchParams}`
  );

  const table_rows = eventsList
    ? eventsList.data.map((event) => ({
        id: event.id,
        entries: [
          <span className="mono-s">
            {new Date(event.time).toLocaleString("en-US", {
              year: "numeric",
              month: "short",
              day: "numeric",
              hour: "numeric",
              minute: "2-digit",
              hour12: true,
            })}
          </span>,
          <span className="mono-s">
            {event.status === "success" ? (
              <Badge text="Successful" success />
            ) : event.status === "failed" ? (
              <Badge text="Failed" danger />
            ) : (
              <Badge text="Pending" />
            )}
          </span>,
          <span className="mono-s">{event.topic}</span>,
          <span className="mono-s">{event.id}</span>,
        ],
      }))
    : [];

  return (
    <div className="destination-events">
      <div className="destination-events__header">
        <h2 className="title-l">
          Events <Badge text={eventsList ? eventsList.data.length + "" : "0"} />
        </h2>
        <div className="destination-events__header-filters">
          {/* <SearchInput
            value={search}
            onChange={setSearch}
            placeholder="Filter by ID"
          /> */}
          {/* <Button>Last 24 Hours</Button>
          <Button>Status</Button>
          <Button>Topics</Button>
          <Button>Refresh</Button> */}
          <Dropdown trigger_icon={<FilterIcon />} trigger={<span>Status</span>}>
            <div className="dropdown-item">
              <Checkbox
                label="Success"
                checked={status.value === "success"}
                onChange={() =>
                  status.value === "success"
                    ? status.set("")
                    : status.set("success")
                }
              />
            </div>
            <div className="dropdown-item">
              <Checkbox
                label="Failed"
                checked={status.value === "failed"}
                onChange={() =>
                  status.value === "failed"
                    ? status.set("")
                    : status.set("failed")
                }
              />
            </div>
          </Dropdown>
        </div>
      </div>

      <div className="destination-events__table">
        <Table
          columns={[
            {
              header: "Timestamp",
            },
            {
              header: "Status",
            },
            {
              header: "Topic",
            },
            {
              header: "Message ID",
            },
          ]}
          rows={table_rows}
          footer_label="events"
          // onClick={console.log}
        />
      </div>
    </div>
  );
};

export default Events;

const useEventFilter = () => {
  const [searchParams, setSearchParams] = useSearchParams();

  const handleFilterChange = (key: string, value: string | null) => {
    setSearchParams((prev) => {
      const params = new URLSearchParams(prev);
      if (value) {
        params.set(key, value);
      } else {
        params.delete(key);
      }
      return params;
    });
  };

  const status = {
    value: searchParams.get("status") || "",
    set: (value: string) => handleFilterChange("status", value || null),
  };

  const urlSearchParams = useMemo(() => {
    return searchParams.toString();
  }, [searchParams]);

  return {
    status,
    urlSearchParams,
  };
};
