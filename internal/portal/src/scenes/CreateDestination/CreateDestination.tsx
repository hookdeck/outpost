import "./CreateDestination.scss";
import Button from "../../common/Button/Button";
import { CloseIcon, Loading } from "../../common/Icons";
import Badge from "../../common/Badge/Badge";
import { useNavigate } from "react-router-dom";
import { useContext, useEffect, useState } from "react";
import { ApiContext } from "../../app";
import { showToast } from "../../common/Toast/Toast";
import useSWR, { mutate } from "swr";
import TopicPicker from "../../common/TopicPicker/TopicPicker";
import { DestinationTypeReference, Filter } from "../../typings/Destination";
import DestinationConfigFields from "../../common/DestinationConfigFields/DestinationConfigFields";
import FilterField from "../../common/FilterField/FilterField";
import { getFormValues } from "../../utils/formHelper";
import CONFIGS from "../../config";

type Step = {
  title: string;
  sidebar_shortname: string;
  description: string;
  isValid: (values: Record<string, any>) => boolean;
  FormFields: (props: {
    defaultValue: Record<string, any>;
    onChange: (value: Record<string, any>) => void;
    destinations?: DestinationTypeReference[];
  }) => React.ReactNode;
  action: string;
};

const EVENT_TOPICS_STEP: Step = {
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
      defaultValue.topics
        ? Array.isArray(defaultValue.topics)
          ? defaultValue.topics
          : defaultValue.topics.split(",")
        : []
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
};

const DESTINATION_TYPE_STEP: Step = {
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
    onChange,
  }: {
    destinations?: DestinationTypeReference[];
    defaultValue: Record<string, any>;
    onChange?: (value: Record<string, any>) => void;
  }) => (
    <div className="destination-types">
      <div className="destination-types__container">
        {destinations?.map((destination) => (
          <label key={destination.type} className="destination-type-option">
            <input
              type="radio"
              name="type"
              value={destination.type}
              required
              className="destination-type-radio"
              defaultChecked={
                defaultValue ? defaultValue.type === destination.type : undefined
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
    </div>
  ),
  action: "Next",
};

const CONFIGURATION_STEP: Step = {
  title: "Configure destination",
  sidebar_shortname: "Configure destination",
  description: "Configure the destination you want to send to your destination",
  isValid: (values: Record<string, any>) => {
    // Check if filter is valid (filterValid is set by onValidChange callback)
    if (values.filterValid === false) {
      return false;
    }
    return true;
  },
  FormFields: ({
    defaultValue,
    destinations,
    onChange,
  }: {
    defaultValue: Record<string, any>;
    destinations?: DestinationTypeReference[];
    onChange?: (value: Record<string, any>) => void;
  }) => {
    const destinationType = destinations?.find(
      (d) => d.type === defaultValue.type
    );
    const [filter, setFilter] = useState<Filter>(defaultValue.filter || null);
    const [showFilter, setShowFilter] = useState(!!defaultValue.filter);
    const [filterValid, setFilterValid] = useState(true);

    useEffect(() => {
      if (onChange) {
        onChange({ ...defaultValue, filter, filterValid });
      }
    }, [filter, filterValid]);

    return (
      <>
        <DestinationConfigFields
          type={destinationType!}
          destination={undefined}
        />
        <div className="filter-section">
          <button
            type="button"
            className="filter-section__toggle"
            onClick={() => setShowFilter(!showFilter)}
          >
            <span className={`filter-section__arrow ${showFilter ? "expanded" : ""}`}>
              &#9654;
            </span>
            <span className="filter-section__label">Event Filter (Optional)</span>
          </button>
          {showFilter && (
            <div className="filter-section__content">
              <p className="body-m muted">
                Add a filter to only receive events that match specific criteria.
                Leave empty to receive all events matching the selected topics.
              </p>
              <FilterField
                value={filter}
                onChange={setFilter}
                onValidChange={setFilterValid}
              />
              <input
                type="hidden"
                name="filter"
                value={filter ? JSON.stringify(filter) : ""}
              />
            </div>
          )}
        </div>
      </>
    );
  },
  action: "Create Destination",
};

export default function CreateDestination() {
  const apiClient = useContext(ApiContext);

  const AVAILABLE_TOPICS = CONFIGS.TOPICS.split(",").filter(Boolean);
  let steps = [EVENT_TOPICS_STEP, DESTINATION_TYPE_STEP, CONFIGURATION_STEP];

  // If there are no topics, skip the first step
  if (AVAILABLE_TOPICS.length === 0 && steps.length === 3) {
    steps = [DESTINATION_TYPE_STEP, CONFIGURATION_STEP];
  }

  const navigate = useNavigate();
  const [currentStepIndex, setCurrentStepIndex] = useState(0);
  const [stepValues, setStepValues] = useState<Record<string, any>>({});
  const [isCreating, setIsCreating] = useState(false);
  const { data: destinations } =
    useSWR<DestinationTypeReference[]>(`destination-types`);
  const [isValid, setIsValid] = useState(false);

  const currentStep = steps[currentStepIndex];
  const nextStep = steps[currentStepIndex + 1] || null;

  // Validate the current step when it changes or stepValues change
  useEffect(() => {
    if (currentStep.isValid) {
      setIsValid(currentStep.isValid(stepValues));
    } else {
      setIsValid(false);
    }
  }, [currentStepIndex, stepValues, currentStep]);

  const createDestination = (values: Record<string, any>) => {
    setIsCreating(true);

    const destination_type = destinations?.find((d) => d.type === values.type);

    let topics: string[];
    if (typeof values.topics === "string") {
      topics = values.topics.split(",").filter(Boolean);
    } else if (typeof values.topics === "undefined") {
      topics = ["*"];
    } else if (Array.isArray(values.topics)) {
      topics = values.topics;
    } else {
      // Default to all topics
      topics = ["*"];
    }

    // Parse filter from JSON string if provided
    let filter: Filter = null;
    if (values.filter) {
      try {
        filter = typeof values.filter === "string" ? JSON.parse(values.filter) : values.filter;
      } catch (e) {
        // Invalid JSON, ignore filter
      }
    }

    apiClient
      .fetch(`destinations`, {
        method: "POST",
        body: JSON.stringify({
          type: values.type,
          topics: topics,
          ...(filter && Object.keys(filter).length > 0 ? { filter } : {}),
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
        <div className="create-destination__step__header">
          <h1 className="title-xl">{currentStep.title}</h1>
          <p className="body-m muted">{currentStep.description}</p>
        </div>
        <form
          key={currentStepIndex}
          onChange={(e) => {
            const formData = new FormData(e.currentTarget);
            const values = Object.fromEntries(formData.entries());
            const allValues = { ...stepValues, ...values };

            if (currentStep.isValid) {
              setIsValid(currentStep.isValid(allValues));
            } else {
              setIsValid(e.currentTarget.checkValidity());
            }
          }}
          onSubmit={(e) => {
            e.preventDefault();
            const form = e.target as HTMLFormElement;
            const values = getFormValues(form);

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
                  setStepValues((prev) => ({ ...prev, ...values }));
                  if (currentStep.isValid) {
                    setIsValid(currentStep.isValid({ ...stepValues, ...values }));
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
