export interface DashboardOverview {
  tenant: {
    id: string;
  };
  destinations: Destination[];
  recentEvents: Event[];
  stats: {
    totalDestinations: number;
    totalEvents: number;
  };
}

export interface Destination {
  id: string;
  type: string;
  config: {
    url?: string;
  };
  topics: string[];
  enabled: boolean;
  createdAt: string;
}

export interface Event {
  id: string;
  topic: string;
  data: Record<string, unknown>;
  createdAt: string;
  status: "pending" | "delivered" | "failed";
}
