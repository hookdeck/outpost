import { useState, useContext } from "react";
import Button from "../../../common/Button/Button";
import TopicPicker from "../../../common/TopicPicker/TopicPicker";
import { useNavigate } from "react-router-dom";

import "./DestinationSettings.scss";
import {
  DeleteIcon,
  DisableIcon,
  RotateIcon,
  SaveIcon,
} from "../../../common/Icons";
import { ApiContext } from "../../../app";
import { mutate } from "swr";
import { showToast } from "../../../common/Toast/Toast";
import {
  Destination,
  DestinationTypeReference,
} from "../../../typings/Destination";
import DestinationConfigFields from "../../../common/DestinationConfigFields/DestinationConfigFields";
import CONFIGS from "../../../config";
import { getFormValues } from "../../../utils/formHelper";

const DestinationSettings = ({
  destination,
  type,
}: {
  destination: Destination;
  type: DestinationTypeReference;
}) => {
  const [selectedTopics, setSelectedTopics] = useState(destination.topics);
  const apiClient = useContext(ApiContext);
  const navigate = useNavigate();

  const [isDisabling, setIsDisabling] = useState(false);
  const [isTopicsSaving, setIsTopicsSaving] = useState(false);
  const [isConfigSaving, setIsConfigSaving] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [isRotatingSecret, setIsRotatingSecret] = useState(false);

  const handleToggleEnabled = () => {
    if (!destination.disabled_at) {
      const confirmed = window.confirm(
        "Are you sure you want to disable this destination? You will no longer receive any events while it is disabled."
      );
      if (!confirmed) return;
    }

    setIsDisabling(true);
    apiClient
      .fetch(
        `destinations/${destination.id}/${
          destination.disabled_at ? "enable" : "disable"
        }`,
        {
          method: "PUT",
        }
      )
      .then((data) => {
        showToast(
          "success",
          `Destination ${data.disabled_at ? "disabled" : "enabled"}`
        );
        mutate(`destinations/${destination.id}`, data, false);
      })
      .catch((error) => {
        showToast(
          "error",
          `${error.message.charAt(0).toUpperCase() + error.message.slice(1)}`
        );
      })
      .finally(() => {
        setIsDisabling(false);
      });
  };

  const handleSubmit = (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    setIsTopicsSaving(true);
    apiClient
      .fetch(`destinations/${destination.id}`, {
        method: "PATCH",
        body: JSON.stringify({
          topics: selectedTopics,
        }),
      })
      .then((data) => {
        showToast("success", "Destination event topics updated.");
        mutate(`destinations/${destination.id}`, data, false);
      })
      .catch((error) => {
        showToast(
          "error",
          `${error.message.charAt(0).toUpperCase() + error.message.slice(1)}`
        );
      })
      .finally(() => {
        setIsTopicsSaving(false);
      });
  };

  const handleConfigSubmit = (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    setIsConfigSaving(true);
    const form = e.target as HTMLFormElement;
    const formValues = getFormValues(form);

    // Split values into config and credentials
    const config: Record<string, string> = {};
    const credentials: Record<string, string> = {};

    Object.entries(formValues).forEach(([key, value]) => {
      if (type.credential_fields.some((field) => field.key === key)) {
        credentials[key] = String(value);
      } else {
        config[key] = String(value);
      }
    });

    apiClient
      .fetch(`destinations/${destination.id}`, {
        method: "PATCH",
        body: JSON.stringify({
          config,
          credentials,
        }),
      })
      .then((data) => {
        showToast("success", "Destination configuration updated.");
        mutate(`destinations/${destination.id}`, data, false);
      })
      .catch((error) => {
        showToast(
          "error",
          `${error.message.charAt(0).toUpperCase() + error.message.slice(1)}`
        );
      })
      .finally(() => {
        setIsConfigSaving(false);
      });
  };

  const handleRotateSecret = () => {
    setIsRotatingSecret(true);
    apiClient
      .fetch(`destinations/${destination.id}`, {
        method: "PATCH",
        body: JSON.stringify({
          credentials: {
            rotate_secret: true,
          },
        }),
      })
      .then((data) => {
        showToast("success", "Secret rotated successfully");
        mutate(`destinations/${destination.id}`, data, false);
      })
      .catch((error) => {
        showToast(
          "error",
          `${error.message.charAt(0).toUpperCase() + error.message.slice(1)}`
        );
      })
      .finally(() => {
        setIsRotatingSecret(false);
      });
  };

  const [isConfigFormValid, setIsConfigFormValid] = useState(false);

  const handleConfigFormValidation = (e: React.FormEvent<HTMLFormElement>) => {
    setIsConfigFormValid(e.currentTarget.checkValidity());
  };

  const handleDelete = () => {
    const confirmed = window.confirm(
      "Are you sure you want to delete this destination? This action cannot be undone and all associated events will be deleted."
    );
    if (!confirmed) return;

    setIsDeleting(true);
    apiClient
      .fetch(`destinations/${destination.id}`, {
        method: "DELETE",
      })
      .then(() => {
        showToast("success", "Destination deleted successfully");
        navigate("/");
      })
      .catch((error) => {
        showToast(
          "error",
          `${error.message.charAt(0).toUpperCase() + error.message.slice(1)}`
        );
      })
      .finally(() => {
        setIsDeleting(false);
      });
  };

  return (
    <div className="destination-settings">
      {CONFIGS.TOPICS && (
        <>
          <div className="destination-settings__topics">
            <h2 className="title-l">Event Topics</h2>
            <form onSubmit={handleSubmit}>
              <TopicPicker
                maxHeight="320px"
                selectedTopics={selectedTopics}
                onTopicsChange={(topics) => {
                  setSelectedTopics(topics);
                }}
              />
              <Button
                className="destination-settings__submit-button"
                type="submit"
                disabled={
                  JSON.stringify(selectedTopics) ===
                    JSON.stringify(destination.topics) ||
                  selectedTopics.length === 0
                }
                loading={isTopicsSaving}
                primary
              >
                <SaveIcon /> Save
              </Button>
            </form>
          </div>
          <hr />
        </>
      )}
      <div>
        <h2 className="title-l">Configuration & Credentials</h2>
        <form
          onSubmit={handleConfigSubmit}
          onChange={handleConfigFormValidation}
        >
          <DestinationConfigFields destination={destination} type={type} />
          <Button
            type="submit"
            primary
            disabled={!isConfigFormValid}
            loading={isConfigSaving}
          >
            <SaveIcon /> Save
          </Button>
        </form>
      </div>

      {type.type === "webhook" && (
        <>
          <hr />
          <div className="destination-settings__actions">
            <h2 className="title-l">Rotate Secret</h2>
            <p className="body-m muted">
              Rotate the destination signing secret. This will generate a new
              secret and invalid the current one after 24 hours.
            </p>
            <Button onClick={handleRotateSecret} loading={isRotatingSecret}>
              <RotateIcon />
              Rotate
            </Button>
          </div>
        </>
      )}
      <hr />
      <div className="destination-settings__actions">
        <h2 className="title-l">
          {destination.disabled_at ? "Enable" : "Disable"} event destination
        </h2>
        <p className="body-m muted">
          {destination.disabled_at
            ? "Enabling an event destination will resume receiving events."
            : "Disabling an event destination will prevent it from receiving events."}
        </p>
        <Button onClick={handleToggleEnabled} loading={isDisabling}>
          <DisableIcon />
          {destination.disabled_at ? "Enable" : "Disable"}
        </Button>
      </div>
      <div className="destination-settings__actions">
        <h2 className="title-l">Delete event destination</h2>
        <p className="body-m muted">
          Deleting an event destination is irreversible. All associated events
          will also be deleted.
        </p>
        <Button onClick={handleDelete} loading={isDeleting} danger>
          <DeleteIcon />
          Delete
        </Button>
      </div>
    </div>
  );
};

export default DestinationSettings;
