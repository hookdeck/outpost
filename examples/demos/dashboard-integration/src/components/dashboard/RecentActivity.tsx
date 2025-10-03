import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/Card";
import { Badge } from "@/components/ui/Badge";
import { formatDateTime } from "@/lib/utils";
import type { Event } from "@/types/dashboard";

interface RecentActivityProps {
  events: Event[];
}

export default function RecentActivity({ events }: RecentActivityProps) {
  if (events.length === 0) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Recent Events</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-gray-500">No recent events to display.</p>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Recent Events</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="space-y-4">
          {events.slice(0, 5).map((event, index) => (
            <div
              key={`${event.id}-${(event as any).destination_id || index}`}
              className="flex items-center justify-between"
            >
              <div className="flex-1">
                <div className="flex items-center space-x-2">
                  <span className="text-sm font-medium">{event.topic}</span>
                  <Badge
                    variant={
                      event.status === "delivered"
                        ? "success"
                        : event.status === "failed"
                        ? "error"
                        : "default"
                    }
                  >
                    {event.status}
                  </Badge>
                </div>
                <p className="text-xs text-gray-500 mt-1">
                  {formatDateTime((event as any).time || event.createdAt)}
                </p>
              </div>
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  );
}
