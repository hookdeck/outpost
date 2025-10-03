"use client";

import { useState, useEffect } from "react";
import type { Destination } from "@/types/dashboard";

export function useDestinations() {
  const [data, setData] = useState<Destination[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    async function fetchDestinations() {
      try {
        const response = await fetch("/api/overview");
        if (!response.ok) {
          throw new Error("Failed to fetch destinations");
        }
        const overview = await response.json();
        if (overview.destinations && Array.isArray(overview.destinations)) {
          setData(overview.destinations);
        } else {
          setData([]);
        }
      } catch (err) {
        setError(err instanceof Error ? err.message : "An error occurred");
        setData([]); // Fallback to empty array on error
      } finally {
        setLoading(false);
      }
    }

    fetchDestinations();
  }, []);

  const refetch = async () => {
    setLoading(true);
    setError(null);
    try {
      const response = await fetch("/api/overview");
      if (!response.ok) {
        throw new Error("Failed to fetch destinations");
      }
      const overview = await response.json();
      if (overview.destinations && Array.isArray(overview.destinations)) {
        setData(overview.destinations);
      } else {
        setData([]);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "An error occurred");
      setData([]); // Fallback to empty array on error
    } finally {
      setLoading(false);
    }
  };

  return { data, loading, error, refetch };
}