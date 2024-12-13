import useSWR from "swr";

export interface DestinationType {
  type: string;
  label: string;
  icon: string | React.ReactNode;
  target: string;
}

const targets = {
  webhook: "url",
  rabbitmq: "exchange",
  aws: "queue_url",
};

export function useDestinationTypes(): Record<string, DestinationType> {
  const { data } = useSWR<Record<string, DestinationType>>("destination-types");
  if (!data) {
    return {};
  }
  return Object.values(data).reduce((acc, type) => {
    acc[type.type] = {
      ...type,
      target: targets[type.type as keyof typeof targets],
    };
    return acc;
  }, {} as Record<string, DestinationType>);
}

export function useDestinationType(type: string): DestinationType | undefined {
  const destination_types = useDestinationTypes();
  return destination_types[type];
}
