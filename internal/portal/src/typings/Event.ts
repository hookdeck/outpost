// Event is stateless - represents the original event without delivery status
interface Event {
  id: string;
  topic: string;
  time: string;
  eligible_for_retry: boolean;
  metadata?: Record<string, string>;
  data?: Record<string, unknown>;
}

// EventSummary is the event object when expand=event (without data)
interface EventSummary {
  id: string;
  topic: string;
  time: string;
  eligible_for_retry: boolean;
  metadata?: Record<string, string>;
}

// EventFull is the event object when expand=event.data
interface EventFull extends EventSummary {
  data?: Record<string, unknown>;
}

// Attempt represents a delivery attempt for an event to a destination
interface Attempt {
  id: string;
  event_id: string;
  destination_id: string;
  status: "success" | "failed";
  delivered_at: string;
  code?: string;
  response_data?: Record<string, unknown>;
  attempt_number: number;
  manual: boolean;
  // Expandable field - only present when using include=event
  event?: EventSummary | EventFull;
}

interface SeekPagination {
  order_by: string;
  dir: "asc" | "desc";
  limit: number;
  next: string | null;
  prev: string | null;
}

interface AttemptListResponse {
  models: Attempt[];
  pagination: SeekPagination;
}

interface EventListResponse {
  models: Event[];
  pagination: SeekPagination;
}

export type {
  Event,
  EventSummary,
  EventFull,
  Attempt,
  SeekPagination,
  AttemptListResponse,
  EventListResponse,
};
