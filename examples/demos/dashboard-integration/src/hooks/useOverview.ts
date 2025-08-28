"use client";

import { useState, useEffect } from "react";
import type { DashboardOverview } from "@/types/dashboard";

export function useOverview() {
  const [data, setData] = useState<DashboardOverview | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    async function fetchOverview() {
      try {
        const response = await fetch("/api/overview");
        if (!response.ok) {
          throw new Error("Failed to fetch overview");
        }
        const overview = await response.json();
        setData(overview);
      } catch (err) {
        setError(err instanceof Error ? err.message : "An error occurred");
      } finally {
        setLoading(false);
      }
    }

    fetchOverview();
  }, []);

  const refetch = async () => {
    setLoading(true);
    setError(null);
    try {
      const response = await fetch("/api/overview");
      if (!response.ok) {
        throw new Error("Failed to fetch overview");
      }
      const overview = await response.json();
      setData(overview);
    } catch (err) {
      setError(err instanceof Error ? err.message : "An error occurred");
    } finally {
      setLoading(false);
    }
  };

  return { data, loading, error, refetch };
}
