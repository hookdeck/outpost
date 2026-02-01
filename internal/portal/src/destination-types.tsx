import { useContext } from "react";
import useSWR from "swr";
import type { DestinationTypeReference } from "./typings/Destination";
import { ApiContext } from "./app";

export function useDestinationTypes(): Record<
  string,
  DestinationTypeReference
> {
  const apiClient = useContext(ApiContext);
  const { data } = useSWR<DestinationTypeReference[]>(
    "destination-types",
    (path: string) => apiClient.fetchRoot(path),
  );
  if (!data) {
    return {};
  }
  return data.reduce(
    (acc, type) => {
      acc[type.type] = type;
      return acc;
    },
    {} as Record<string, DestinationTypeReference>,
  );
}

export function useDestinationType(
  type: string | undefined,
): DestinationTypeReference | undefined {
  const destination_types = useDestinationTypes();

  if (!type) {
    return undefined;
  }

  return destination_types[type];
}
