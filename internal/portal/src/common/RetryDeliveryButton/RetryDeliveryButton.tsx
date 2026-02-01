import React, { useCallback, useContext, useState, type MouseEvent } from "react";
import Button from "../Button/Button";
import { ReplayIcon } from "../Icons";
import { showToast } from "../Toast/Toast";
import { ApiContext, formatError } from "../../app";

interface RetryDeliveryButtonProps {
  eventId: string;
  destinationId: string;
  disabled: boolean;
  loading: boolean;
  completed: (success: boolean) => void;
  icon?: boolean;
  iconLabel?: string;
}

const RetryDeliveryButton: React.FC<RetryDeliveryButtonProps> = ({
  eventId,
  destinationId,
  disabled,
  loading,
  completed,
  icon,
  iconLabel,
}) => {
  const apiClient = useContext(ApiContext);
  const [retrying, setRetrying] = useState<boolean>(false);

  const retryDelivery = useCallback(
    async (e: MouseEvent<HTMLButtonElement>) => {
      e.stopPropagation();
      setRetrying(true);
      try {
        await apiClient.fetchRoot("retry", {
          method: "POST",
          body: JSON.stringify({
            event_id: eventId,
            destination_id: destinationId,
          }),
        });
        showToast("success", "Retry successful.");
        completed(true);
      } catch (error: unknown) {
        showToast("error", "Retry failed. " + formatError(error));
        completed(false);
      }

      setRetrying(false);
    },
    [apiClient, eventId, destinationId, completed],
  );

  return (
    <Button
      minimal
      icon={icon}
      iconLabel={iconLabel}
      onClick={(e) => retryDelivery(e)}
      disabled={disabled || retrying}
      loading={loading || retrying}
    >
      <ReplayIcon />
    </Button>
  );
};

export default RetryDeliveryButton;
