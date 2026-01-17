import React, { useCallback, useContext, useState, MouseEvent } from "react";
import Button from "../Button/Button";
import { ReplayIcon } from "../Icons";
import { showToast } from "../Toast/Toast";
import { ApiContext } from "../../app";

interface RetryDeliveryButtonProps {
  deliveryId: string;
  disabled: boolean;
  loading: boolean;
  completed: (success: boolean) => void;
  icon?: boolean;
  iconLabel?: string;
}

const RetryDeliveryButton: React.FC<RetryDeliveryButtonProps> = ({
  deliveryId,
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
        await apiClient.fetch(`deliveries/${deliveryId}/retry`, {
          method: "POST",
        });
        showToast("success", "Retry successful.");
        completed(true);
      } catch (error: any) {
        showToast(
          "error",
          "Retry failed. " +
            `${error.message.charAt(0).toUpperCase() + error.message.slice(1)}`,
        );
        completed(false);
      }

      setRetrying(false);
    },
    [apiClient, deliveryId, completed],
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
