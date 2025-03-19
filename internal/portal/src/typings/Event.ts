interface Event {
  id: string;
  tenant_id: string;
  destination_id: string;
  topic: string;
  time: Date;
  status: "success" | "failed" | "pending";
  metadata: any;
  data: any;
}

interface EventListResponse {
  data: Event[];
  next?: string;
  previous?: string;
}

export type { Event, EventListResponse };
