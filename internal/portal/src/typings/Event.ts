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

interface Delivery {
  id: string;
  delivered_at: Date;
  status: "success" | "failed";
  code: string;
  response_data: any;
}

interface EventListResponse {
  data: Event[];
  next?: string;
  prev?: string;
  count: number;
}

export type { Event, EventListResponse, Delivery };
