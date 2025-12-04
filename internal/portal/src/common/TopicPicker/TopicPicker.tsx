import { useState, useMemo } from "react";

import "./TopicPicker.scss";
import { Checkbox } from "../Checkbox/Checkbox";
import CONFIGS from "../../config";
import SearchInput from "../SearchInput/SearchInput";
import { DropdownIcon } from "../Icons";

interface Topic {
  id: string;
  category: string;
}

interface TopicPickerProps {
  maxHeight?: string;
  selectedTopics: string[];
  onTopicsChange: (topics: string[]) => void;
}

const detectSeparator = (topics: string[]): string => {
  // Common separators to check
  const possibleSeparators = ["/", ".", "-"];

  // Find the first separator that appears in all topics
  // and is the first occurring separator in each topic
  return (
    possibleSeparators.find((sep) =>
      topics.every((topic) => {
        const sepIndex = topic.indexOf(sep);
        if (sepIndex === -1) return false;

        // Check if any other separator appears before this one
        const otherSepsIndex = possibleSeparators
          .filter((s) => s !== sep)
          .map((s) => topic.indexOf(s))
          .filter((idx) => idx !== -1);

        return otherSepsIndex.every((idx) => idx === -1 || idx > sepIndex);
      })
    ) || "-"
  ); // Fallback to '-' if no consistent separator is found
};

const topics: Topic[] = (() => {
  const topicsList = CONFIGS.TOPICS.split(",");
  const separator = detectSeparator(topicsList);

  return topicsList.map((topic) => {
    const parts = topic.split(separator);
    return {
      id: topic,
      category: parts[0],
    };
  });
})();

const TopicPicker = ({
  maxHeight,
  selectedTopics,
  onTopicsChange,
}: TopicPickerProps) => {
  const [searchQuery, setSearchQuery] = useState("");
  const [expandedCategories, setExpandedCategories] = useState<string[]>(
    Array.from(new Set(topics.map((topic) => topic.category)))
  );

  const isEverythingSelected = selectedTopics.includes("*");

  const toggleSelectAll = () => {
    if (isEverythingSelected) {
      onTopicsChange([]);
    } else {
      onTopicsChange(["*"]);
    }
  };

  // Group topics by category
  const categorizedTopics = useMemo(() => {
    const filtered = topics.filter((topic) =>
      topic.id.toLowerCase().includes(searchQuery.toLowerCase())
    );

    return filtered.reduce((acc, topic) => {
      const category = topic.category;
      if (!acc[category]) {
        acc[category] = [];
      }
      acc[category].push(topic);
      return acc;
    }, {} as Record<string, Topic[]>);
  }, [topics, searchQuery]);

  const toggleCategory = (category: string) => {
    setExpandedCategories((prev) =>
      prev.includes(category)
        ? prev.filter((c) => c !== category)
        : [...prev, category]
    );
  };

  const toggleCategorySelection = (topicsInCategory: Topic[]) => {
    if (isEverythingSelected) {
      selectedTopics = [];
    }

    const categoryTopicIds = topicsInCategory.map((t) => t.id);
    const areAllSelected = categoryTopicIds.every((id) =>
      selectedTopics.includes(id)
    );

    if (areAllSelected) {
      onTopicsChange(
        selectedTopics.filter((id) => !categoryTopicIds.includes(id))
      );
    } else {
      const newSelected = new Set([...selectedTopics, ...categoryTopicIds]);
      onTopicsChange(Array.from(newSelected));
    }
  };

  const toggleTopic = (topicId: string) => {
    if (isEverythingSelected) {
      selectedTopics = [];
    }

    const newSelected = selectedTopics.includes(topicId)
      ? selectedTopics.filter((id) => id !== topicId)
      : [...selectedTopics, topicId];
    onTopicsChange(newSelected);
  };

  return (
    <div className="topic-picker" style={{ maxHeight: maxHeight }}>
      <div className="topic-picker__header">
        <SearchInput
          value={searchQuery}
          onChange={(value) => setSearchQuery(value)}
          placeholder="Filter topics..."
        />
      </div>
      <div className="topic-picker__content">
        {searchQuery.length === 0 && (
          <div className="topic-picker__select-all">
            <Checkbox
              label="Select All"
              checked={isEverythingSelected}
              onChange={toggleSelectAll}
            />
          </div>
        )}
        {Object.entries(categorizedTopics).length === 0 && (
          <span className="body-m muted">No topics match your filter.</span>
        )}
        {Object.entries(categorizedTopics).map(([category, categoryTopics]) => {
          const isExpanded = expandedCategories.includes(category);
          const selectedCount = categoryTopics.filter((topic) =>
            selectedTopics.includes(topic.id)
          ).length;
          const areAllSelected = selectedCount === categoryTopics.length;
          const isIndeterminate = selectedCount > 0 && !areAllSelected;

          return (
            <div key={category} className="topic-picker__category">
              <div className="topic-picker__category-header">
                <button
                  type="button"
                  onClick={() => toggleCategory(category)}
                  className="topic-picker__category-toggle"
                >
                  <span className={`arrow ${isExpanded ? "expanded" : ""}`}>
                    <DropdownIcon />
                  </span>
                </button>
                <Checkbox
                  label={`${category
                    .split(" ")
                    .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
                    .join(" ")}`}
                  checked={isEverythingSelected || areAllSelected}
                  indeterminate={!isEverythingSelected && isIndeterminate}
                  onChange={() => toggleCategorySelection(categoryTopics)}
                  disabled={isEverythingSelected}
                />
              </div>
              {isExpanded && (
                <div className="topic-picker__topics">
                  {categoryTopics.map((topic) => (
                    <div key={topic.id} className="topic-picker__topic">
                      <Checkbox
                        checked={
                          isEverythingSelected ||
                          selectedTopics.indexOf(topic.id) !== -1
                        }
                        onChange={() => toggleTopic(topic.id)}
                        label={topic.id}
                        monospace
                        disabled={isEverythingSelected}
                      />
                    </div>
                  ))}
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
};

export default TopicPicker;
