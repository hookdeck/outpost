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

// Delivery represents a delivery attempt for an event to a destination
interface Delivery {
  id: string;
  status: "success" | "failed";
  delivered_at: string;
  code?: string;
  response_data?: Record<string, unknown>;
  attempt: number;
  // Expandable fields - string (ID) or object depending on expand param
  event: string | EventSummary | EventFull;
  destination: string;
}

interface DeliveryListResponse {
  data: Delivery[];
  next?: string;
  prev?: string;
  count?: number;
}

interface EventListResponse {
  data: Event[];
  next?: string;
  prev?: string;
  count?: number;
}

export type {
  Event,
  EventSummary,
  EventFull,
  Delivery,
  DeliveryListResponse,
  EventListResponse,
};
