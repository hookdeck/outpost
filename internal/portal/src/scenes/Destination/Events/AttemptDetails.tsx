import { useParams } from "react-router-dom";
import Button from "../../../common/Button/Button";
import { CloseIcon } from "../../../common/Icons";
import useSWR from "swr";
import { Attempt, EventFull } from "../../../typings/Event";
import Badge from "../../../common/Badge/Badge";
import RetryDeliveryButton from "../../../common/RetryDeliveryButton/RetryDeliveryButton";
import { CopyButton } from "../../../common/CopyButton/CopyButton";

const AttemptDetails = ({
  navigateAttempt,
}: {
  navigateAttempt: (path: string, params?: any) => void;
}) => {
  const { attempt_id: attemptId, destination_id: destinationId } = useParams();

  const { data: attempt } = useSWR<Attempt>(
    `destinations/${destinationId}/attempts/${attemptId}?include=event.data,response_data`,
  );

  if (!attempt) {
    return <div>Loading...</div>;
  }

  const event = attempt.event ? (attempt.event as EventFull) : null;

  return (
    <div className="drawer">
      <div className="drawer__header">
        <h3 className="drawer__header-title mono-s">
          {event?.topic || "Attempt"}
        </h3>
        <div className="drawer__header-actions">
          <RetryDeliveryButton
            eventId={attempt.event_id}
            destinationId={attempt.destination_id}
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
            onClick={() => navigateAttempt("/")}
          >
            <CloseIcon />
          </Button>
        </div>
      </div>

      <div className="drawer__body">
        <div className="attempt-data">
          <div className="attempt-data__section">
            <dl className="body-m description-list">
              <div>
                <dt>Status</dt>
                <dd>
                  <Badge
                    text={
                      attempt.status === "success" ? "Successful" : "Failed"
                    }
                    success={attempt.status === "success"}
                    danger={attempt.status === "failed"}
                  />
                </dd>
              </div>
              {attempt.code && (
                <div>
                  <dt>Response Code</dt>
                  <dd className="mono-s">{attempt.code}</dd>
                </div>
              )}
              <div>
                <dt>Attempt</dt>
                <dd className="mono-s">{attempt.attempt_number}</dd>
              </div>
              {event && (
                <div>
                  <dt>Topic</dt>
                  <dd className="mono-s">{event.topic}</dd>
                </div>
              )}
              <div>
                <dt>Delivered at</dt>
                <dd className="mono-s time">
                  {new Date(attempt.delivered_at).toLocaleString("en-US", {
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
              <div>
                <dt>Attempt ID</dt>
                <dd className="mono-s id-field">
                  <span>{attempt.id}</span>
                  <CopyButton value={attempt.id} />
                </dd>
              </div>
              {event && (
                <div>
                  <dt>Event ID</dt>
                  <dd className="mono-s id-field">
                    <span>{event.id}</span>
                    <CopyButton value={event.id} />
                  </dd>
                </div>
              )}
            </dl>
          </div>

          {event?.data && (
            <div className="attempt-data__section">
              <h3 className="subtitle-m">Data</h3>
              <pre className="mono-s">
                {JSON.stringify(event.data, null, 2)}
              </pre>
            </div>
          )}

          {event?.metadata && Object.keys(event.metadata).length > 0 && (
            <div className="attempt-data__section">
              <h3 className="subtitle-m">Metadata</h3>
              <pre className="mono-s">
                {JSON.stringify(event.metadata, null, 2)}
              </pre>
            </div>
          )}

          {attempt.response_data && (
            <div className="attempt-data__section">
              <h3 className="subtitle-m">Response</h3>
              <pre className="mono-s">
                {JSON.stringify(attempt.response_data, null, 2)}
              </pre>
            </div>
          )}
        </div>
      </div>
    </div>
  );
};

export default AttemptDetails;
