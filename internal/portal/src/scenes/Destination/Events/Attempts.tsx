import { useCallback, useMemo, useState } from "react";
import Badge from "../../../common/Badge/Badge";
import Button from "../../../common/Button/Button";
import "./Attempts.scss";
import Table from "../../../common/Table/Table";
import type { AttemptListResponse, EventSummary } from "../../../typings/Event";
import useSWR from "swr";
import Dropdown from "../../../common/Dropdown/Dropdown";
import {
  CalendarIcon,
  FilterIcon,
  PreviousIcon,
  RefreshIcon,
  NextIcon,
} from "../../../common/Icons";
import RetryDeliveryButton from "../../../common/RetryDeliveryButton/RetryDeliveryButton";
import { Checkbox } from "../../../common/Checkbox/Checkbox";
import {
  Route,
  Routes,
  useNavigate,
  useSearchParams,
  Outlet,
  useParams,
} from "react-router-dom";
import CONFIGS from "../../../config";
import AttemptDetails from "./AttemptDetails";

interface AttemptsProps {
  destination: any;
  navigateAttempt: (path: string, state?: any) => void;
}

const Attempts: React.FC<AttemptsProps> = ({
  destination,
  navigateAttempt,
}) => {
  const [timeRange, setTimeRange] = useState("24h");
  const { attempt_id: attemptId } = useParams<{ attempt_id: string }>();
  const { status, topics, pagination, urlSearchParams } = useAttemptFilter();

  const queryUrl = useMemo(() => {
    const searchParams = new URLSearchParams(urlSearchParams);

    const now = new Date();
    switch (timeRange) {
      case "7d":
        searchParams.set(
          "start",
          new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000).toISOString(),
        );
        break;
      case "30d":
        searchParams.set(
          "start",
          new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000).toISOString(),
        );
        break;
      default: // 24h
        searchParams.set(
          "start",
          new Date(now.getTime() - 24 * 60 * 60 * 1000).toISOString(),
        );
    }

    if (!searchParams.has("limit")) {
      searchParams.set("limit", "15");
    }

    searchParams.set("include", "event");

    return `destinations/${destination.id}/attempts?${searchParams.toString()}`;
  }, [destination.id, timeRange, urlSearchParams]);

  const {
    data: attemptsList,
    mutate,
    isValidating,
  } = useSWR<AttemptListResponse>(queryUrl, {
    revalidateOnFocus: false,
  });

  const topicsList = CONFIGS.TOPICS.split(",");

  const table_rows = attemptsList?.models
    ? attemptsList.models.map((attempt) => {
        const event = attempt.event ? (attempt.event as EventSummary) : null;
        return {
          id: attempt.id,
          active: attempt.id === (attemptId || ""),
          entries: [
            <span className="mono-s attempt-time-cell">
              {new Date(attempt.delivered_at).toLocaleString("en-US", {
                month: "short",
                day: "numeric",
                hour: "numeric",
                minute: "2-digit",
                hour12: true,
              })}
            </span>,
            <span className="mono-s">
              {attempt.status === "success" ? (
                <Badge text="Successful" success />
              ) : (
                <Badge text="Failed" danger />
              )}
              <RetryDeliveryButton
                eventId={attempt.event_id}
                destinationId={attempt.destination_id}
                disabled={isValidating}
                loading={isValidating}
                completed={(success) => {
                  if (success) {
                    mutate();
                  }
                }}
              />
            </span>,
            <span className="mono-s">{event?.topic || "-"}</span>,
            <span className="mono-s">{attempt.id}</span>,
          ],
          onClick: () => navigateAttempt(`/${attempt.id}`),
        };
      })
    : [];

  return (
    <div className="destination-attempts">
      <div className="destination-attempts__header">
        <h2 className="destination-attempts__header-title title-l">
          Event Deliveries{" "}
          <Badge text={attemptsList?.models.length ?? 0} size="s" />
        </h2>
        <div className="destination-attempts__header-filters">
          <Dropdown
            trigger_icon={<CalendarIcon />}
            trigger={`Last ${timeRange}`}
          >
            <div className="dropdown-item">
              <Checkbox
                label="Last 24h"
                checked={timeRange === "24h"}
                onChange={() => {
                  setTimeRange("24h");
                  pagination.clear();
                }}
              />
            </div>
            <div className="dropdown-item">
              <Checkbox
                label="Last 7d"
                checked={timeRange === "7d"}
                onChange={() => {
                  setTimeRange("7d");
                  pagination.clear();
                }}
              />
            </div>
            <div className="dropdown-item">
              <Checkbox
                label="Last 30d"
                checked={timeRange === "30d"}
                onChange={() => {
                  setTimeRange("30d");
                  pagination.clear();
                }}
              />
            </div>
          </Dropdown>

          <Dropdown
            trigger_icon={<FilterIcon />}
            trigger="Status"
            badge_count={status.value ? 1 : 0}
            badge_variant="primary"
          >
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

          <Dropdown
            trigger_icon={<FilterIcon />}
            trigger="Topics"
            badge_count={topics.value.length}
            badge_variant="primary"
          >
            {topicsList.map((topic) => (
              <div key={topic} className="dropdown-item">
                <Checkbox
                  label={topic}
                  checked={topics.value.includes(topic)}
                  onChange={() => topics.toggle(topic)}
                />
              </div>
            ))}
          </Dropdown>

          <Button
            onClick={() => mutate()}
            disabled={isValidating}
            loading={isValidating}
          >
            <RefreshIcon />
            Refresh
          </Button>
        </div>
      </div>

      <div
        className={`destination-attempts__table ${
          attemptId ? "destination-attempts__table--active" : ""
        }`}
      >
        <Table
          columns={[
            {
              header: "Delivered At",
              width: 160,
            },
            {
              header: "Status",
              width: 160,
            },
            {
              header: "Topic",
            },
            {
              header: "Attempt ID",
            },
          ]}
          rows={table_rows}
          footer={
            <div className="table__footer">
              <div>
                <span className="subtitle-s">
                  {attemptsList?.models.length ?? 0} attempts
                </span>
              </div>

              <nav>
                <Button
                  icon
                  iconLabel="Previous"
                  disabled={!attemptsList?.pagination?.prev}
                  onClick={() =>
                    pagination.prev(attemptsList?.pagination?.prev || "")
                  }
                >
                  <PreviousIcon />
                </Button>
                <Button
                  icon
                  iconLabel="Next"
                  disabled={!attemptsList?.pagination?.next}
                  onClick={() =>
                    pagination.next(attemptsList?.pagination?.next || "")
                  }
                >
                  <NextIcon />
                </Button>
              </nav>
            </div>
          }
        />

        <Outlet />
      </div>
    </div>
  );
};

export default Attempts;

const useAttemptFilter = () => {
  const [searchParams, setSearchParams] = useSearchParams();

  const handleFilterChange = (key: string, value: string | null) => {
    setSearchParams((prev) => {
      const params = new URLSearchParams(prev);
      // Clear pagination
      params.delete("next");
      params.delete("prev");
      // Set new filter
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

  const topics = {
    value: searchParams.getAll("topic"),
    set: (value: string[]) => {
      setSearchParams((prev) => {
        const params = new URLSearchParams(prev);
        // Clear pagination
        params.delete("next");
        params.delete("prev");
        // Set new filter
        params.delete("topic");
        value.forEach((v) => params.append("topic", v));
        return params;
      });
    },
    toggle: (topic: string) => {
      const currentTopics = searchParams.getAll("topic");
      const newTopics = currentTopics.includes(topic)
        ? currentTopics.filter((t) => t !== topic)
        : [...currentTopics, topic];
      setSearchParams((prev) => {
        const params = new URLSearchParams(prev);
        // Clear pagination
        params.delete("next");
        params.delete("prev");
        // Set new filter
        params.delete("topic");
        newTopics.forEach((t) => params.append("topic", t));
        return params;
      });
    },
  };

  const pagination = {
    next: (cursor: string) => {
      setSearchParams((prev) => {
        const params = new URLSearchParams(prev);
        params.delete("prev");
        params.set("next", cursor);
        return params;
      });
    },
    prev: (cursor: string) => {
      setSearchParams((prev) => {
        const params = new URLSearchParams(prev);
        params.delete("next");
        params.set("prev", cursor);
        return params;
      });
    },
    clear: () => {
      setSearchParams((prev) => {
        const params = new URLSearchParams(prev);
        params.delete("next");
        params.delete("prev");
        return params;
      });
    },
  };

  const urlSearchParams = useMemo(() => {
    return searchParams.toString();
  }, [searchParams]);

  return {
    status,
    topics,
    pagination,
    urlSearchParams,
  };
};

export const AttemptRoutes = ({ destination }: { destination: any }) => {
  const { urlSearchParams } = useAttemptFilter();
  const navigate = useNavigate();

  const navigateAttempt = useCallback(
    (path: string, state?: any) => {
      navigate(
        `/destinations/${destination.id}/deliveries${path}?${urlSearchParams}`,
        { state },
      );
    },
    [navigate, destination.id, urlSearchParams],
  );

  return (
    <Routes>
      <Route
        path="/"
        element={
          <Attempts
            destination={destination}
            navigateAttempt={navigateAttempt}
          />
        }
      >
        <Route
          path=":attempt_id/*"
          element={<AttemptDetails navigateAttempt={navigateAttempt} />}
        />
      </Route>
    </Routes>
  );
};
