import { useContext, useMemo } from "react";
import useSWR from "swr";
import { ApiContext } from "./app";
import type { TopicDeprecation } from "./typings/Destination";
import { parseTopicDeprecations } from "./utils/topics";

// useTopicDeprecations returns the deprecated topics by name, from v2
// GET /topics. It only enriches CONFIGS.TOPICS, which stays the topic list, so
// a failed request just shows no badges.
export function useTopicDeprecations(): Map<string, TopicDeprecation> {
  const apiClient = useContext(ApiContext);
  const { data } = useSWR<TopicDeprecation[]>(
    "topics",
    (path: string) => apiClient.fetchRoot(path).then(parseTopicDeprecations),
    {
      revalidateIfStale: false,
      revalidateOnFocus: false,
      shouldRetryOnError: false,
    },
  );
  return useMemo(
    () => new Map((data ?? []).map((topic) => [topic.name, topic])),
    [data],
  );
}
