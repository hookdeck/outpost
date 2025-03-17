import "./CreateDestination.scss";
import Button from "../../common/Button/Button";
import { CloseIcon, Loading } from "../../common/Icons";
import Badge from "../../common/Badge/Badge";
import { useNavigate } from "react-router-dom";
import { useContext, useEffect, useRef, useState } from "react";
import { ApiContext } from "../../app";
import { showToast } from "../../common/Toast/Toast";
import useSWR, { mutate } from "swr";
import TopicPicker from "../../common/TopicPicker/TopicPicker";
import { DestinationTypeReference } from "../../typings/Destination";
import DestinationConfigFields from "../../common/DestinationConfigFields/DestinationConfigFields";

const steps = [
  {
    title: "Select event topics",
    sidebar_shortname: "Event topics",
    description: "Select the event topics you want to send to your destination",
    isValid: (values: Record<string, any>) => {
      if (values.topics?.length > 0) {
        return true;
      }
      return false;
    },
    FormFields: ({
      defaultValue,
      onChange,
    }: {
      defaultValue: Record<string, any>;
      onChange: (value: Record<string, any>) => void;
    }) => {
      const [selectedTopics, setSelectedTopics] = useState<string[]>(
        defaultValue.topics ? defaultValue.topics.split(",") : []
      );

      useEffect(() => {
        onChange({ topics: selectedTopics });
      }, [selectedTopics]);

      return (
        <>
          <TopicPicker
            selectedTopics={selectedTopics}
            onTopicsChange={setSelectedTopics}
          />
          <input
            readOnly
            type="text"
            name="topics"
            hidden
            required
            value={selectedTopics.length > 0 ? selectedTopics.join(",") : ""}
          />
        </>
      );
    },
    action: "Next",
  },
  {
    title: "Select destination type",
    sidebar_shortname: "Destination type",
    description:
      "Select the destination type you want to send to your destination",
    isValid: (values: Record<string, any>) => {
      if (!values.type) {
        return false;
      }
      return true;
    },
    FormFields: ({
      destinations,
      defaultValue,
    }: {
      destinations: DestinationTypeReference[];
      defaultValue: Record<string, any>;
    }) => (
      <div className="destination-types">
        {destinations?.sort((a, b) => a.label.localeCompare(b.label)).map((destination, i) => (
          <label key={destination.type} className="destination-type-card">
          <input
            type="radio"
            name="type"
            value={destination.type}
            required
            className="destination-type-radio"
            defaultChecked={
              defaultValue
            ? defaultValue.type === destination.type
            : undefined
            }
          />
          <div className="destination-type-content">
            <h3 className="subtitle-l">
              <span
            className="destination-type-content__icon"
            dangerouslySetInnerHTML={{ __html: destination.icon }}
              />{" "}
              {destination.label}
            </h3>
            <p className="body-m muted">{destination.description}</p>
          </div>
            </label>
          ))}
        </div>
    ),
    action: "Next",
  },
  {
    title: "Configure destination",
    sidebar_shortname: "Configure destination",
    description:
      "Configure the destination you want to send to your destination",
    FormFields: ({
      defaultValue,
      destinations,
    }: {
      defaultValue: Record<string, any>;
      destinations: DestinationTypeReference[];
    }) => {
      const destinationType = destinations?.find(
        (d) => d.type === defaultValue.type
      );
      return (
        <DestinationConfigFields
          type={destinationType!}
          destination={undefined}
        />
      );
    },
    action: "Create Destination",
  },
];

export default function CreateDestination() {
  const apiClient = useContext(ApiContext);

  const navigate = useNavigate();
  const [currentStepIndex, setCurrentStepIndex] = useState(0);
  const [stepValues, setStepValues] = useState<Record<string, any>>({});
  const [isCreating, setIsCreating] = useState(false);
  const { data: destinations } =
    useSWR<DestinationTypeReference[]>(`destination-types`);
  const [isValid, setIsValid] = useState(false);

  const currentStep = steps[currentStepIndex];
  const nextStep = steps[currentStepIndex + 1] || null;

  const createDestination = (values: Record<string, any>) => {
    setIsCreating(true);

    const destination_type = destinations?.find((d) => d.type === values.type);

    apiClient
      .fetch(`destinations`, {
        method: "POST",
        body: JSON.stringify({
          type: values.type,
          topics: values.topics.split(","),
          config: Object.fromEntries(
            Object.entries(values).filter(([key]) =>
              destination_type?.config_fields.some((field) => field.key === key)
            )
          ),
          credentials: Object.fromEntries(
            Object.entries(values).filter(([key]) =>
              destination_type?.credential_fields.some(
                (field) => field.key === key
              )
            )
          ),
        }),
      })
      .then((data) => {
        showToast("success", `Destination created`);
        mutate(`destinations/${data.id}`, data, false);
        navigate(`/destinations/${data.id}`);
      })
      .catch((error) => {
        showToast(
          "error",
          `${error.message.charAt(0).toUpperCase() + error.message.slice(1)}`
        );
      })
      .finally(() => {
        setIsCreating(false);
      });
  };

  return (
    <div className="create-destination">
      <div className="create-destination__sidebar">
        <Button to="/" minimal>
          <CloseIcon /> Cancel
        </Button>
        <div className="create-destination__sidebar__steps">
          {steps.map((step, index) => (
            <button
              key={index}
              disabled={index > currentStepIndex}
              onClick={() => setCurrentStepIndex(index)}
              className={`create-destination__sidebar__steps__step ${
                currentStepIndex === index ? "active" : ""
              }`}
            >
              <Badge
                text={`${index + 1}`}
                primary={currentStepIndex === index}
              />{" "}
              {step.sidebar_shortname}
            </button>
          ))}
        </div>
      </div>

      <div className="create-destination__step">
        <h1 className="title-xl">{currentStep.title}</h1>
        <p className="body-m muted">{currentStep.description}</p>
        <form
          key={currentStepIndex}
          onChange={(e) => {
            const formData = new FormData(e.currentTarget);
            const values = Object.fromEntries(formData.entries());

            if (currentStep.isValid) {
              setIsValid(currentStep.isValid(values));
            } else {
              setIsValid(e.currentTarget.checkValidity());
            }
          }}
          onSubmit={(e) => {
            e.preventDefault();
            const formData = new FormData(e.target as HTMLFormElement);
            const values = Object.fromEntries(formData.entries());

            const newValues = { ...stepValues, ...values };
            if (nextStep) {
              setStepValues(newValues);
              setCurrentStepIndex(currentStepIndex + 1);
            } else {
              createDestination(newValues);
            }
          }}
        >
          <div className="create-destination__step__fields">
            {destinations ? (
              <currentStep.FormFields
                defaultValue={stepValues}
                destinations={destinations}
                onChange={(values) => {
                  if (currentStep.isValid) {
                    setIsValid(currentStep.isValid(values));
                  }
                }}
              />
            ) : (
              <div>
                <Loading />
              </div>
            )}
          </div>
          <div className="create-destination__step__actions">
            <Button
              disabled={!isValid}
              primary
              type="submit"
              loading={isCreating}
            >
              {currentStep.action}
            </Button>
          </div>
        </form>
      </div>
    </div>
  );
}
