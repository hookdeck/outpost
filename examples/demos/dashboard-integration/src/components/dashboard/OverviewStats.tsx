import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/Card";
import { Badge } from "@/components/ui/Badge";
import Link from "next/link";

interface OverviewStatsProps {
  stats: {
    totalDestinations: number;
  };
}

export default function OverviewStats({ stats }: OverviewStatsProps) {
  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-sm font-medium">
            Total Destinations
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold">{stats.totalDestinations}</div>
          <p className="text-xs text-gray-500 mt-1">
            Active event destinations
          </p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-sm font-medium">Status</CardTitle>
        </CardHeader>
        <CardContent>
          <Badge variant="success">Active</Badge>
          <p className="text-xs text-gray-500 mt-1">All systems operational</p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-sm font-medium">Quick Actions</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="space-y-2">
            <Link
              href="/dashboard/event-destinations"
              className="block text-sm text-blue-600 hover:text-blue-500"
            >
              Manage destinations →
            </Link>
            <Link
              href="/dashboard/event-destinations/new"
              className="block text-sm text-blue-600 hover:text-blue-500"
            >
              Create destination →
            </Link>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
