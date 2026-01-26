import React, { useCallback, useContext, useState, MouseEvent } from "react";
import Button from "../Button/Button";
import { ReplayIcon } from "../Icons";
import { showToast } from "../Toast/Toast";
import { ApiContext, formatError } from "../../app";

interface RetryAttemptButtonProps {
  attemptId: string;
  disabled: boolean;
  loading: boolean;
  completed: (success: boolean) => void;
  icon?: boolean;
  iconLabel?: string;
}

const RetryAttemptButton: React.FC<RetryAttemptButtonProps> = ({
  attemptId,
  disabled,
  loading,
  completed,
  icon,
  iconLabel,
}) => {
  const apiClient = useContext(ApiContext);
  const [retrying, setRetrying] = useState<boolean>(false);

  const retryAttempt = useCallback(
    async (e: MouseEvent<HTMLButtonElement>) => {
      e.stopPropagation();
      setRetrying(true);
      try {
        await apiClient.fetch(`attempts/${attemptId}/retry`, {
          method: "POST",
        });
        showToast("success", "Retry successful.");
        completed(true);
      } catch (error: unknown) {
        showToast("error", "Retry failed. " + formatError(error));
        completed(false);
      }

      setRetrying(false);
    },
    [apiClient, attemptId, completed],
  );

  return (
    <Button
      minimal
      icon={icon}
      iconLabel={iconLabel}
      onClick={(e) => retryAttempt(e)}
      disabled={disabled || retrying}
      loading={loading || retrying}
    >
      <ReplayIcon />
    </Button>
  );
};

export default RetryAttemptButton;
