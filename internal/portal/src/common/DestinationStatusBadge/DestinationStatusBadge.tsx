import Badge from "../Badge/Badge";
import type { Destination } from "../../typings/Destination";
import { destinationStatus } from "../../utils/destinationTypes";

// DestinationStatusBadge shows Active, Disabled or, for destinations past
// their expires_at, Expired.
const DestinationStatusBadge = ({
  destination,
  now = Date.now(),
}: {
  destination: Pick<Destination, "disabled_at" | "expires_at">;
  now?: number;
}) => {
  switch (destinationStatus(destination, now)) {
    case "expired":
      return <Badge text="Expired" />;
    case "disabled":
      return <Badge text="Disabled" />;
    default:
      return <Badge text="Active" success />;
  }
};

export default DestinationStatusBadge;
