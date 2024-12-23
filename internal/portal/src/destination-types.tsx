import useSWR from "swr";
import { DestinationTypeReference } from "./typings/Destination";

export function useDestinationTypes(): Record<
  string,
  DestinationTypeReference
> {
  const { data } = useSWR<DestinationTypeReference[]>("destination-types");
  if (!data) {
    return {};
  }
  return data.reduce((acc, type) => {
    acc[type.type] = type;
    return acc;
  }, {} as Record<string, DestinationTypeReference>);
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
