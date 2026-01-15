import { useParams } from "react-router-dom";
import Button from "../../../common/Button/Button";
import { CloseIcon } from "../../../common/Icons";
import useSWR from "swr";
import { Delivery, EventFull } from "../../../typings/Event";
import Badge from "../../../common/Badge/Badge";
import RetryDeliveryButton from "../../../common/RetryDeliveryButton/RetryDeliveryButton";

const DeliveryDetails = ({
  navigateDelivery,
}: {
  navigateDelivery: (path: string, params?: any) => void;
}) => {
  const { delivery_id: deliveryId } = useParams();

  const { data: delivery } = useSWR<Delivery>(
    `deliveries/${deliveryId}?expand=event.data,response_data`
  );

  if (!delivery) {
    return <div>Loading...</div>;
  }

  const event = typeof delivery.event === "object" ? delivery.event as EventFull : null;

  return (
    <div className="drawer">
      <div className="drawer__header">
        <div className="drawer__header-actions">
          <Badge
            text={delivery.status === "success" ? "Successful" : "Failed"}
            success={delivery.status === "success"}
            danger={delivery.status === "failed"}
          />

          <RetryDeliveryButton
            deliveryId={delivery.id}
            disabled={false}
            loading={false}
            completed={() => {}}
            icon
            iconLabel="Retry"
          />

          <Button
            icon
            iconLabel="Close"
            minimal
            onClick={() => navigateDelivery("/")}
          >
            <CloseIcon />
          </Button>
        </div>
      </div>

      <div className="drawer__body">
        <div className="delivery-data">
          <div className="delivery-data__overview">
            <h3 className="subtitle-m">Delivery</h3>
            <dl className="body-m description-list">
              <div>
                <dt>ID</dt>
                <dd className="mono-s">{delivery.id}</dd>
              </div>
              <div>
                <dt>Status</dt>
                <dd>
                  <Badge
                    text={delivery.status === "success" ? "Successful" : "Failed"}
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
              {delivery.code && (
                <div>
                  <dt>Response Code</dt>
                  <dd className="mono-s">{delivery.code}</dd>
                </div>
              )}
              <div>
                <dt>Attempt</dt>
                <dd className="mono-s">{delivery.attempt}</dd>
              </div>
            </dl>
          </div>

          {event && (
            <div className="delivery-data__event">
              <h3 className="subtitle-m">Event</h3>
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
          )}

          {event?.data && (
            <div className="delivery-data__data">
              <h3 className="subtitle-m">Event Data</h3>
              <pre className="mono-s">{JSON.stringify(event.data, null, 2)}</pre>
            </div>
          )}

          {event?.metadata && Object.keys(event.metadata).length > 0 && (
            <div className="delivery-data__metadata">
              <h3 className="subtitle-m">Event Metadata</h3>
              <pre className="mono-s">
                {JSON.stringify(event.metadata, null, 2)}
              </pre>
            </div>
          )}

          {delivery.response_data && (
            <div className="delivery-data__response">
              <h3 className="subtitle-m">Response</h3>
              <pre className="mono-s">
                {JSON.stringify(delivery.response_data, null, 2)}
              </pre>
            </div>
          )}
        </div>
      </div>
    </div>
  );
};

export default DeliveryDetails;
