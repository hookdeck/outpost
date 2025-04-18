import { Route, Routes, useLocation, useParams } from "react-router-dom";
import Button from "../../../common/Button/Button";
import {
  ArrowForwardIcon,
  CloseIcon,
  ArrowBackIcon,
} from "../../../common/Icons";
import useSWR from "swr";
import { Event, Delivery } from "../../../typings/Event";
import Badge from "../../../common/Badge/Badge";
import RetryEventButton from "../../../common/RetryEventButton/RetryEventButton";

const EventDetails = ({
  navigateEvent,
}: {
  navigateEvent: (path: string, params?: any) => void;
}) => {
  const { destination_id: destinationId, event_id: eventId } = useParams();

  const { data: event } = useSWR<Event>(
    `destinations/${destinationId}/events/${eventId}`
  );

  if (!event) {
    return <div>Error loading event</div>;
  }

  return (
    <Routes>
      <Route
        path="/"
        element={<EventData event={event} navigateEvent={navigateEvent} />}
      />
      <Route
        path="/attempts"
        element={<EventAttempts navigateEvent={navigateEvent} />}
      />
      <Route
        path="/attempts/:delivery_id"
        element={
          <EventAttemptDetails event={event} navigateEvent={navigateEvent} />
        }
      />
    </Routes>
  );
};

export default EventDetails;

const EventData = ({
  event,
  navigateEvent,
}: {
  event: Event;
  navigateEvent: (path: string, params?: any) => void;
}) => {
  const { event_id: eventId } = useParams();

  return (
    <div className="drawer">
      <div className="drawer__header">
        <div className="drawer__header-tabs">
          <Button
            minimal
            className="active"
            onClick={() => navigateEvent(`/${eventId}`)}
          >
            Event
          </Button>
          <Button minimal onClick={() => navigateEvent(`/${eventId}/attempts`)}>
            Delivery Attempts
          </Button>
        </div>

        <div className="drawer__header-actions">
          <Badge
            text={event.status === "success" ? "Successful" : "Failed"}
            success={event.status === "success"}
            danger={event.status === "failed"}
          />

          <RetryEventButton
            eventId={event.id}
            destinationId={event.destination_id}
            disabled={["success", "failed"].includes(event.status) === false}
            loading={false}
            completed={(success) => {
              // TODO: completed should be optional
            }}
          />

          <Button
            icon
            iconLabel="Close"
            minimal
            onClick={() => navigateEvent("/")}
          >
            <CloseIcon />
          </Button>
        </div>
      </div>

      <div className="drawer__body">
        <div className="event-data">
          <div className="event-data__overview">
            <h3 className="subtitle-m">Overview</h3>
            <dl className="body-m description-list">
              <div>
                <dt>ID</dt>
                <dd className="mono-s">{event.id}</dd>
              </div>
              <div>
                <dt>Topic</dt>
                <dd className="mono-s">{event.topic}</dd>
              </div>
              <div>
                <dt>Status</dt>
                <dd>
                  <Badge
                    text={event.status === "success" ? "Successful" : "Failed"}
                    success={event.status === "success"}
                    danger={event.status === "failed"}
                  />
                </dd>
              </div>
              <div>
                <dt>Received at</dt>
                <dd className="mono-s time">
                  {new Date(event.time).toLocaleString("en-US", {
                    year: "numeric",
                    month: "numeric",
                    day: "numeric",
                    hour: "numeric",
                    minute: "2-digit",
                    second: "2-digit",
                    timeZoneName: "short",
                  })}
                </dd>
              </div>
            </dl>
          </div>

          <div className="event-data__data">
            <h3 className="subtitle-m">Data</h3>
            <pre className="mono-s">{JSON.stringify(event.data, null, 2)}</pre>
          </div>

          <div className="event-data__metadata">
            <h3 className="subtitle-m">Metadata</h3>
            <pre className="mono-s">
              {JSON.stringify(event.metadata, null, 2)}
            </pre>
          </div>
        </div>
      </div>
    </div>
  );
};

const EventAttempts = ({
  navigateEvent,
}: {
  navigateEvent: (path: string, params?: any) => void;
}) => {
  const { event_id: eventId, destination_id: destinationId } = useParams();

  const { data: deliveries } = useSWR<Delivery[]>(
    `events/${eventId}/deliveries?destination_id=${destinationId}`
  );

  return (
    <div className="drawer">
      <div className="drawer__header">
        <div className="drawer__header-tabs">
          <Button minimal onClick={() => navigateEvent(`/${eventId}`)}>
            Event
          </Button>
          <Button
            minimal
            className="active"
            onClick={() => navigateEvent(`/${eventId}/attempts`)}
          >
            Delivery Attempts
          </Button>
        </div>

        <div>
          <Button
            icon
            iconLabel="Close"
            minimal
            onClick={() => navigateEvent("/")}
          >
            <CloseIcon />
          </Button>
        </div>
      </div>

      <div className="drawer__body">
        <div className="event-attempts">
          <ol className="event-attempts__list">
            {deliveries?.map((delivery, index) => {
              const deliveryIndex = deliveries.length - index;
              return (
                <li
                  key={String(delivery.delivered_at)}
                  className="event-attempts__list-item"
                >
                  <button
                    type="button"
                    onClick={() => {
                      navigateEvent(`/${eventId}/attempts/${delivery.id}`, {
                        delivery,
                        deliveryIndex,
                      });
                    }}
                  >
                    <span className="visually-hidden">Go to attempt</span>
                  </button>

                  <span className="event-attempts__list-item-content">
                    <span>
                      <span className="title-m">Attempt {deliveryIndex}</span>
                      <span className="mono-s">
                        {new Date(delivery.delivered_at).toLocaleString(
                          "en-US",
                          {
                            month: "short",
                            day: "numeric",
                            hour: "numeric",
                            minute: "2-digit",
                            hour12: true,
                          }
                        )}
                      </span>
                    </span>
                    <span>
                      <Badge
                        text={
                          delivery.status === "success"
                            ? "Successful"
                            : "Failed"
                        }
                        success={delivery.status === "success"}
                        danger={delivery.status === "failed"}
                      />
                      <span className="icon-container">
                        <ArrowForwardIcon />
                      </span>
                    </span>
                  </span>
                </li>
              );
            })}
          </ol>
        </div>
      </div>
    </div>
  );
};

const EventAttemptDetails = ({
  event,
  navigateEvent,
}: {
  event: Event;
  navigateEvent: (path: string, params?: any) => void;
}) => {
  const { deliveryIndex, delivery } = useLocation().state as {
    deliveryIndex: number;
    delivery: Delivery;
  };

  return (
    <div className="drawer">
      <div className="drawer__header">
        <div className="drawer__header-tabs">
          <Button
            minimal
            onClick={() => navigateEvent(`/${event.id}/attempts`)}
          >
            <ArrowBackIcon />
            Attempts {deliveryIndex}
          </Button>
        </div>

        <div className="drawer__header-actions">
          <Badge
            text={delivery.status === "success" ? "Successful" : "Failed"}
            success={delivery.status === "success"}
            danger={delivery.status === "failed"}
          />

          <Button
            icon
            iconLabel="Close"
            minimal
            onClick={() => navigateEvent("/")}
          >
            <CloseIcon />
          </Button>
        </div>
      </div>

      <div className="drawer__body">
        <div className="event-attempt-details">
          <div className="event-attempt-details__overview">
            <h3 className="subtitle-m">Overview</h3>

            <dl className="body-m description-list">
              <div>
                <dt>ID</dt>
                <dd className="mono-s">{delivery.id}</dd>
              </div>
              <div>
                <dt>Status</dt>
                <dd>
                  <Badge
                    text={
                      delivery.status === "success" ? "Successful" : "Failed"
                    }
                    success={delivery.status === "success"}
                    danger={delivery.status === "failed"}
                  />
                </dd>
              </div>
              <div>
                <dt>Delivered at</dt>
                <dd className="mono-s time">
                  {new Date(delivery.delivered_at).toLocaleString("en-US", {
                    year: "numeric",
                    month: "numeric",
                    day: "numeric",
                    hour: "numeric",
                    minute: "2-digit",
                    second: "2-digit",
                    timeZoneName: "short",
                  })}
                </dd>
              </div>
            </dl>
          </div>

          {/* Currently, we're not storing the exact message data sent. */}
          <div className="event-attempt-details__message">
            <h3 className="subtitle-m">Message</h3>
            <pre className="mono-s">
              {JSON.stringify(
                {
                  metadata: event.metadata,
                  data: event.data,
                },
                null,
                2
              )}
            </pre>
          </div>

          <div className="event-attempt-details__result">
            <h3 className="subtitle-m">Response</h3>
            <pre className="mono-s">
              {JSON.stringify(delivery.response_data, null, 2)}
            </pre>
          </div>
        </div>
      </div>
    </div>
  );
};
