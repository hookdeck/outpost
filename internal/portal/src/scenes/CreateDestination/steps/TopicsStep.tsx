import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import Button from "../../../common/Button/Button";
import TopicPicker from "../../../common/TopicPicker/TopicPicker";
import { useCreateDestinationContext } from "../CreateDestination";

export default function TopicsStep() {
  const { stepValues, setStepValues, nextPath, buildSearchParams } =
    useCreateDestinationContext();
  const navigate = useNavigate();

  const [selectedTopics, setSelectedTopics] = useState<string[]>(
    stepValues.topics
      ? Array.isArray(stepValues.topics)
        ? stepValues.topics
        : stepValues.topics.split(",")
      : [],
  );

  const isValid = selectedTopics.length > 0;

  useEffect(() => {
    setStepValues((prev) => ({ ...prev, topics: selectedTopics }));
  }, [selectedTopics, setStepValues]);

  return (
    <>
      <div className="create-destination__step__header">
        <h1 className="title-xl">Select event topics</h1>
        <p className="body-m muted">
          Select the event topics you want to send to your destination
        </p>
      </div>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (isValid && nextPath) {
            navigate(
              nextPath +
                buildSearchParams({ topics: selectedTopics.join(",") }),
            );
          }
        }}
      >
        <div className="create-destination__step__fields">
          <TopicPicker
            selectedTopics={selectedTopics}
            onTopicsChange={setSelectedTopics}
          />
        </div>
        <div className="create-destination__step__actions">
          <Button disabled={!isValid} primary type="submit">
            Next
          </Button>
        </div>
      </form>
    </>
  );
}
