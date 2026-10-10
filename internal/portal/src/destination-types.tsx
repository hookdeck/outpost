import { useContext, useMemo } from "react";
import useSWR from "swr";
import type { DestinationTypeReference } from "./typings/Destination";
import { ApiContext } from "./app";
import { SHOW_MCP_DESTINATIONS } from "./config";
import { indexTypes, lookupType, visibleTypes } from "./utils/destinationTypes";

const NO_TYPES: DestinationTypeReference[] = [];

function useDestinationTypeList(): DestinationTypeReference[] | undefined {
  const apiClient = useContext(ApiContext);
  const { data } = useSWR<DestinationTypeReference[]>(
    "destination-types",
    (path: string) => apiClient.fetchRoot(path),
    { revalidateIfStale: false },
  );
  if (data === undefined) {
    return undefined;
  }
  return Array.isArray(data) ? data : NO_TYPES;
}

// useAllDestinationTypes returns every type the API lists, including those
// the portal hides. Use it to recognize hidden destinations, never to offer a
// type.
export function useAllDestinationTypes():
  | Record<string, DestinationTypeReference>
  | undefined {
  const list = useDestinationTypeList();
  return useMemo(() => (list ? indexTypes(list) : undefined), [list]);
}

// useDestinationTypes returns the types the portal shows: MCP types are
// dropped when SHOW_MCP_DESTINATIONS is false.
export function useDestinationTypes():
  | Record<string, DestinationTypeReference>
  | undefined {
  const list = useDestinationTypeList();
  return useMemo(
    () =>
      list ? indexTypes(visibleTypes(list, SHOW_MCP_DESTINATIONS)) : undefined,
    [list],
  );
}

export function useDestinationType(
  type: string | undefined,
): DestinationTypeReference | undefined {
  const destination_types = useDestinationTypes();
  return lookupType(destination_types, type);
}
