import { useState } from "react";
import Badge from "../../../common/Badge/Badge";
import Button from "../../../common/Button/Button";
import SearchInput from "../../../common/SearchInput/SearchInput";
import "./Events.scss";
import Table from "../../../common/Table/Table";
import {EventListResponse} from "../../../typings/Event";
import useSWR from "swr";

const Events = ({ destination }: { destination: any }) => {
  const [search, setSearch] = useState("");

  // const { data: events } = useSWR<EventType[]>(
  //   `destinations/${destination.id}/events`
  // );

  const { data: eventsList } = useSWR<EventListResponse>(
    `events?destination_id=${destination.id}`
  );

  const table_rows = eventsList? eventsList.data.map((event) => ({
    id: event.id,
    entries: [
      <span className="mono-s">{new Date(event.time).toLocaleString(
        "en-US",
        {
          year: "numeric",
          month: "short",
          day: "numeric",
          hour: "numeric",
          minute: "2-digit",
          hour12: true,
        }
      )}</span>,
      <span className="mono-s">{event.successful_at ?  <Badge text="Active" success /> :  <Badge text="Failed" danger />}</span>,
      <span className="mono-s">
        {event.topic}
      </span>,
      <span className="mono-s">
      {event.id}
      </span>,
    ],
  })) : [];

  return (
    <div className="destination-events">
      <div className="destination-events__header">
        <h2 className="title-l">
          Events <Badge text={eventsList ? eventsList.data.length + "" : "0"} />
        </h2>
        {/* <div className="destination-events__header-filters">
          <SearchInput
            value={search}
            onChange={setSearch}
            placeholder="Filter by ID"
          />
          <Button>Last 24 Hours</Button>
          <Button>Status</Button>
          <Button>Topics</Button>
          <Button>Refresh</Button>
        </div> */}
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
