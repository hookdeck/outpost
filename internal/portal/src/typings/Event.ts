interface Event {
  id: string;
  destination_id: string;
  topic: string;
  time: Date;
  successful_at: Date;
  metadata: any;
  data: any;
}

interface EventListResponse {
  data: Event[];
  next?: string;
  previous?: string;
}

export type {
  Event,
  EventListResponse
};
