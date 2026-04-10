import { useContext, useEffect, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import Button from "../../../common/Button/Button";
import DestinationConfigFields from "../../../common/DestinationConfigFields/DestinationConfigFields";
import FilterField from "../../../common/FilterField/FilterField";
import { FilterSyntaxGuide } from "../../../common/FilterSyntaxGuide/FilterSyntaxGuide";
import {
  AddIcon,
  CloseIcon,
  HelpIcon,
  Loading,
} from "../../../common/Icons";
import { useSidebar } from "../../../common/Sidebar/Sidebar";
import type { Filter } from "../../../typings/Destination";
import { getFormValues } from "../../../utils/formHelper";
import { ApiContext, formatError } from "../../../app";
import { showToast } from "../../../common/Toast/Toast";
import { mutate } from "swr";
import CONFIGS from "../../../config";
import { useCreateDestinationContext } from "../CreateDestination";

export default function ConfigStep() {
  const {
    stepValues,
    setStepValues,
    destinationTypes,
    hasDestinationTypes,
    steps,
  } = useCreateDestinationContext();
  const apiClient = useContext(ApiContext);
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const sidebar = useSidebar();

  // Hydrate type from URL search params if context is empty (page refresh)
  const type = stepValues.type || searchParams.get("type");
  const destinationType = destinationTypes[type];
  const [filter, setFilter] = useState<Filter>(stepValues.filter || null);
  const [showFilter, setShowFilter] = useState(!!stepValues.filter);
  const [filterValid, setFilterValid] = useState(true);
  const [isCreating, setIsCreating] = useState(false);

  const isFilterEnabled = CONFIGS.ENABLE_DESTINATION_FILTER === "true";

  // Redirect to first step if type is missing from both context and URL
  useEffect(() => {
    if (hasDestinationTypes && !type) {
      navigate(`/new/${steps[0].path}`, { replace: true });
    }
  }, [hasDestinationTypes, type, navigate, steps]);

  useEffect(() => {
    setStepValues((prev) => ({ ...prev, filter, filterValid }));
  }, [filter, filterValid, setStepValues]);

  const isValid = filterValid;

  const createDestination = (formValues: Record<string, any>) => {
    // Merge search params as fallback for values lost from context (e.g. page refresh)
    const topicsFromUrl = searchParams.get("topics");
    const values = {
      ...(topicsFromUrl ? { topics: topicsFromUrl } : {}),
      type,
      ...stepValues,
      ...formValues,
    };
    setIsCreating(true);

    const destination_type = destinationTypes[values.type];

    let topics: string[];
    if (typeof values.topics === "string") {
      topics = values.topics.split(",").filter(Boolean);
    } else if (typeof values.topics === "undefined") {
      topics = ["*"];
    } else if (Array.isArray(values.topics)) {
      topics = values.topics;
    } else {
      topics = ["*"];
    }

    let parsedFilter: Filter = null;
    if (values.filter) {
      try {
        parsedFilter =
          typeof values.filter === "string"
            ? JSON.parse(values.filter)
            : values.filter;
      } catch {
        // Invalid JSON, ignore filter
      }
    }

    apiClient
      .fetch(`destinations`, {
        method: "POST",
        body: JSON.stringify({
          type: values.type,
          topics: topics,
          ...(parsedFilter && Object.keys(parsedFilter).length > 0
            ? { filter: parsedFilter }
            : {}),
          config: Object.fromEntries(
            Object.entries(values)
              .filter(([key]) =>
                destination_type?.config_fields.some(
                  (field) => field.key === key,
                ),
              )
              .map(([key, value]) => [key, String(value)]),
          ),
          credentials: Object.fromEntries(
            Object.entries(values).filter(([key]) =>
              destination_type?.credential_fields.some(
                (field) => field.key === key,
              ),
            ),
          ),
        }),
      })
      .then((data) => {
        showToast("success", `Destination created`);
        mutate(`destinations/${data.id}`, data, false);
        navigate(`/destinations/${data.id}`);
      })
      .catch((error) => {
        showToast("error", formatError(error));
      })
      .finally(() => {
        setIsCreating(false);
      });
  };

  if (!destinationType && hasDestinationTypes && !type) {
    return null; // Redirecting
  }

  return (
    <>
      <div className="create-destination__step__header">
        <h1 className="title-xl">Configure destination</h1>
        <p className="body-m muted">
          Configure the destination you want to send to your destination
        </p>
      </div>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          const form = e.target as HTMLFormElement;
          const values = getFormValues(form);
          createDestination(values);
        }}
      >
        <div className="create-destination__step__fields">
          {hasDestinationTypes && destinationType ? (
            <>
              <DestinationConfigFields
                type={destinationType}
                destination={undefined}
              />
              {isFilterEnabled && (
                <div className="filter-section">
                  <div className="filter-section__toggle-container">
                    {showFilter ? (
                      <>
                        <p className="subtitle-s">Event Filter</p>
                        <button
                          type="button"
                          className="filter-section__toggle"
                          onClick={() => setShowFilter(!showFilter)}
                        >
                          <CloseIcon />
                          <span className="filter-section__label">Remove</span>
                        </button>
                      </>
                    ) : (
                      <button
                        type="button"
                        className="filter-section__toggle"
                        onClick={() => setShowFilter(!showFilter)}
                      >
                        <AddIcon />
                        <span className="filter-section__label">
                          Add Event Filter
                        </span>
                      </button>
                    )}
                  </div>
                  {showFilter && (
                    <div className="filter-section__content">
                      <p className="body-m muted">
                        Add a filter to only receive events that match specific
                        criteria. Leave empty to receive all events matching the
                        selected topics.
                      </p>
                      <Button
                        type="button"
                        onClick={() =>
                          sidebar.toggle(
                            "filter-syntax",
                            <FilterSyntaxGuide />,
                          )
                        }
                        className="filter-section__guide-button"
                      >
                        <HelpIcon />
                        Filter Syntax Guide
                      </Button>
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
              )}
            </>
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
            Create Destination
          </Button>
        </div>
      </form>
    </>
  );
}
