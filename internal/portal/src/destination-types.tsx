import useSWR from "swr";
import { DestinationTypeReference } from "./typings/Destination";

export function useDestinationTypes(): Record<
  string,
  DestinationTypeReference
> {
  const { data } =
    useSWR<Record<string, DestinationTypeReference>>("destination-types");
  if (!data) {
    return {};
  }
  return data;
}

export function useDestinationType(
  type: string | undefined
): DestinationTypeReference | undefined {
  const destination_types = useDestinationTypes();

  if (!type) {
    return undefined;
  }

  return destination_types[type];
}
