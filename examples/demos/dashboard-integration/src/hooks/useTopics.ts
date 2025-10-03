"use client";

import { useState, useEffect } from "react";

export function useTopics() {
  const [data, setData] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    async function fetchTopics() {
      try {
        const response = await fetch("/api/topics");
        if (!response.ok) {
          throw new Error("Failed to fetch topics");
        }
        const topics = await response.json();
        setData(Array.isArray(topics) ? topics : []);
      } catch (err) {
        setError(err instanceof Error ? err.message : "An error occurred");
        setData([]); // Fallback to empty array on error
      } finally {
        setLoading(false);
      }
    }

    fetchTopics();
  }, []);

  const refetch = async () => {
    setLoading(true);
    setError(null);
    try {
      const response = await fetch("/api/topics");
      if (!response.ok) {
        throw new Error("Failed to fetch topics");
      }
      const topics = await response.json();
      setData(Array.isArray(topics) ? topics : []);
    } catch (err) {
      setError(err instanceof Error ? err.message : "An error occurred");
      setData([]); // Fallback to empty array on error
    } finally {
      setLoading(false);
    }
  };

  return { data, loading, error, refetch };
}