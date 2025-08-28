"use client";

import { useSession } from "next-auth/react";
import Link from "next/link";
import { useOverview } from "@/hooks/useOverview";
import OverviewStats from "@/components/dashboard/OverviewStats";
import RecentActivity from "@/components/dashboard/RecentActivity";

export default function DashboardPage() {
  const { data: session } = useSession();
  const { data: overview, loading, error } = useOverview();

  if (loading) {
    return (
      <div className="space-y-6">
        <div>
          <h1 className="text-2xl font-bold text-gray-900">Dashboard</h1>
          <p className="text-gray-600">
            Loading your event destinations overview...
          </p>
        </div>
        <div className="animate-pulse">
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
            {[...Array(4)].map((_, i) => (
              <div
                key={i}
                className="bg-white p-6 rounded-lg border border-gray-200"
              >
                <div className="h-4 bg-gray-200 rounded w-3/4 mb-2"></div>
                <div className="h-8 bg-gray-200 rounded w-1/2"></div>
              </div>
            ))}
          </div>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="space-y-6">
        <div>
          <h1 className="text-2xl font-bold text-gray-900">Dashboard</h1>
          <p className="text-red-600">Error loading dashboard: {error}</p>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-gray-900">Dashboard</h1>
        <p className="text-gray-600">
          Welcome back, {session?.user?.name || session?.user?.email}
        </p>
      </div>

      {overview && (
        <>
          <OverviewStats stats={overview.stats} />

          <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
            <RecentActivity events={overview.recentEvents} />

            <div className="bg-white p-6 rounded-lg border border-gray-200">
              <h3 className="text-lg font-medium text-gray-900 mb-4">
                Destinations ({overview.destinations.length})
              </h3>
              {overview.destinations.length === 0 ? (
                <p className="text-sm text-gray-500">
                  No destinations configured yet.{" "}
                  <Link
                    href="/dashboard/event-destinations"
                    className="text-blue-600 hover:text-blue-500"
                  >
                    Add your first destination →
                  </Link>
                </p>
              ) : (
                <div className="space-y-2">
                  {overview.destinations.map((dest) => (
                    <a
                      key={dest.id}
                      href={`/dashboard/event-destinations/destinations/${dest.id}`}
                      className="flex items-center justify-between p-2 bg-gray-50 rounded hover:bg-gray-100 transition-colors cursor-pointer"
                    >
                      <div>
                        <span className="text-sm font-medium text-gray-900">
                          {dest.type}
                        </span>
                        {dest.config.url && (
                          <p className="text-xs text-gray-600">
                            {dest.config.url}
                          </p>
                        )}
                      </div>
                      <span
                        className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${
                          dest.enabled
                            ? "bg-green-100 text-green-800"
                            : "bg-gray-100 text-gray-800"
                        }`}
                      >
                        {dest.enabled ? "Active" : "Inactive"}
                      </span>
                    </a>
                  ))}
                  <div className="pt-2">
                    <Link
                      href="/dashboard/event-destinations"
                      className="text-sm text-blue-600 hover:text-blue-500"
                    >
                      Manage all destinations →
                    </Link>
                  </div>
                </div>
              )}
            </div>
          </div>
        </>
      )}
    </div>
  );
}
