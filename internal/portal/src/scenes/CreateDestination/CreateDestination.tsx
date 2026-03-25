import "./CreateDestination.scss";
import Button from "../../common/Button/Button";
import { CloseIcon } from "../../common/Icons";
import Badge from "../../common/Badge/Badge";
import {
  Routes,
  Route,
  Navigate,
  useLocation,
  useNavigate,
  useSearchParams,
} from "react-router-dom";
import { useState, useMemo, useCallback, useEffect, createContext, useContext } from "react";
import { DestinationTypeReference } from "../../typings/Destination";
import { useDestinationTypes } from "../../destination-types";
import CONFIGS from "../../config";
import TopicsStep from "./steps/TopicsStep";
import TypeStep from "./steps/TypeStep";
import ConfigStep from "./steps/ConfigStep";

type StepDef = {
  path: string;
  sidebar_shortname: string;
};

const TOPICS_STEP: StepDef = {
  path: "topics",
  sidebar_shortname: "Event topics",
};

const TYPE_STEP: StepDef = {
  path: "type",
  sidebar_shortname: "Destination type",
};

const CONFIG_STEP: StepDef = {
  path: "config",
  sidebar_shortname: "Configure destination",
};

export type CreateDestinationContextValue = {
  stepValues: Record<string, any>;
  setStepValues: React.Dispatch<React.SetStateAction<Record<string, any>>>;
  destinationTypes: Record<string, DestinationTypeReference>;
  hasDestinationTypes: boolean;
  nextPath: string | null;
  steps: StepDef[];
  buildSearchParams: (extra?: Record<string, string>) => string;
};

const CreateDestinationContext =
  createContext<CreateDestinationContextValue | null>(null);

export function useCreateDestinationContext(): CreateDestinationContextValue {
  const ctx = useContext(CreateDestinationContext);
  if (!ctx) {
    throw new Error(
      "useCreateDestinationContext must be used within CreateDestination",
    );
  }
  return ctx;
}

export default function CreateDestination() {
  const AVAILABLE_TOPICS = CONFIGS.TOPICS.split(",").filter(Boolean);
  const steps = useMemo(() => {
    if (AVAILABLE_TOPICS.length === 0) {
      return [TYPE_STEP, CONFIG_STEP];
    }
    return [TOPICS_STEP, TYPE_STEP, CONFIG_STEP];
  }, [AVAILABLE_TOPICS.length]);

  const location = useLocation();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const destinationTypes = useDestinationTypes();
  const hasDestinationTypes = Object.keys(destinationTypes).length > 0;

  // Hydrate stepValues from URL search params on mount (supports page refresh)
  const [stepValues, setStepValues] = useState<Record<string, any>>(() => {
    const initial: Record<string, any> = {};
    const topicsParam = searchParams.get("topics");
    if (topicsParam) {
      initial.topics = topicsParam.split(",").filter(Boolean);
    }
    const typeParam = searchParams.get("type");
    if (typeParam) {
      initial.type = typeParam;
    }
    return initial;
  });
  const [maxReachedIndex, setMaxReachedIndex] = useState(0);

  // Derive current step index from URL
  const currentStepIndex = useMemo(() => {
    const currentPath = location.pathname.split("/new/")[1]?.split("/")[0];
    const index = steps.findIndex((s) => s.path === currentPath);
    return index >= 0 ? index : 0;
  }, [location.pathname, steps]);

  // Update max reached step when navigating forward
  useEffect(() => {
    if (currentStepIndex > maxReachedIndex) {
      setMaxReachedIndex(currentStepIndex);
    }
  }, [currentStepIndex, maxReachedIndex]);

  // Compute next step path for child components
  const nextPath = useMemo(() => {
    const nextStep = steps[currentStepIndex + 1];
    return nextStep ? `/new/${nextStep.path}` : null;
  }, [steps, currentStepIndex]);

  // Build search params string from current stepValues, with optional extras
  const buildSearchParams = useCallback(
    (extra?: Record<string, string>) => {
      const params = new URLSearchParams();
      const topics = extra?.topics ?? stepValues.topics;
      if (topics) {
        const topicsStr = Array.isArray(topics) ? topics.join(",") : topics;
        if (topicsStr) params.set("topics", topicsStr);
      }
      const type = extra?.type ?? stepValues.type;
      if (type) params.set("type", type);
      const qs = params.toString();
      return qs ? `?${qs}` : "";
    },
    [stepValues],
  );

  const handleSidebarClick = useCallback(
    (index: number) => {
      navigate(`/new/${steps[index].path}${buildSearchParams()}`);
    },
    [navigate, steps, buildSearchParams],
  );

  const contextValue = useMemo(
    () => ({
      stepValues,
      setStepValues,
      destinationTypes,
      hasDestinationTypes,
      nextPath,
      steps,
      buildSearchParams,
    }),
    [stepValues, setStepValues, destinationTypes, hasDestinationTypes, nextPath, steps, buildSearchParams],
  );

  return (
    <CreateDestinationContext.Provider value={contextValue}>
      <div className="create-destination">
        <div className="create-destination__sidebar">
          <Button to="/" minimal>
            <CloseIcon /> Cancel
          </Button>
          <div className="create-destination__sidebar__steps">
            {steps.map((step, index) => (
              <button
                key={step.path}
                disabled={index > maxReachedIndex}
                onClick={() => handleSidebarClick(index)}
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
          <Routes>
            <Route path="topics" element={<TopicsStep />} />
            <Route path="type" element={<TypeStep />} />
            <Route path="config" element={<ConfigStep />} />
            <Route
              path="*"
              element={<Navigate to={`/new/${steps[0].path}`} replace />}
            />
          </Routes>
        </div>
      </div>
    </CreateDestinationContext.Provider>
  );
}
