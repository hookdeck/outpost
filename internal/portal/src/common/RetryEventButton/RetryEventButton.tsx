import React, { useCallback, useContext, useState, MouseEvent } from 'react';
import Button from "../Button/Button";
import { ReplayIcon } from "../Icons";
import { showToast } from "../Toast/Toast";
import { ApiContext } from '../../app';

interface RetryEventButtonProps {
    eventId: string;
    destinationId: string;
    disabled: boolean;
    loading: boolean;
    completed: (success: boolean) => void;
}

const RetryEventButton: React.FC<RetryEventButtonProps> = ({
    eventId,
    destinationId,
    disabled,
    loading,
    completed
}) => {
  const apiClient = useContext(ApiContext);
  const [retrying, setRetrying] = useState<boolean>(false);

    const retryEvent = useCallback(async (e: MouseEvent<HTMLButtonElement>) => {
        e.stopPropagation();
        setRetrying(true);
        try {
          await apiClient.fetch(`destinations/${destinationId}/events/${eventId}/retry`,
            {
              method: "POST",
            }
          );
          showToast("success", "Retry successful.");
          completed(true);
        } catch (error: any) {
          showToast("error", "Retry failed. " + `${error.message.charAt(0).toUpperCase() + error.message.slice(1)}`);
          completed(false);
        }

        setRetrying(false);

      }, [apiClient, destinationId, eventId, completed]);

    return (
        <Button
          minimal
          onClick={(e) => retryEvent(e)}
          disabled={disabled || retrying}
          loading={loading || retrying}
        >
          <ReplayIcon />
        </Button>
    );
};

export default RetryEventButton;