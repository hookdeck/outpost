import { useState, useMemo, useEffect } from "react";

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

const possibleSeparators = ["/", ".", "-"];

const findFirstSeparator = (topic: string): string | null => {
  let firstIndex = -1;
  let firstSep: string | null = null;

  for (const sep of possibleSeparators) {
    const idx = topic.indexOf(sep);
    if (idx !== -1 && (firstIndex === -1 || idx < firstIndex)) {
      firstIndex = idx;
      firstSep = sep;
    }
  }

  return firstSep;
};

const TopicPicker = ({
  maxHeight,
  selectedTopics,
  onTopicsChange,
}: TopicPickerProps) => {
  // Keep track of any custom topics seen during this component's lifecycle
  // so they don't disappear if they are temporarily unselected or "Select All" is clicked.
  const [seenTopics, setSeenTopics] = useState<string[]>([]);

  useEffect(() => {
    setSeenTopics((prev) => {
      const next = new Set(prev);
      let changed = false;
      for (const t of selectedTopics) {
        if (t !== "*" && !next.has(t)) {
          next.add(t);
          changed = true;
        }
      }
      return changed ? Array.from(next) : prev;
    });
  }, [selectedTopics]);

  // Combine statically configured topics with any custom topics already selected
  const allTopics: Topic[] = useMemo(() => {
    const configuredTopics = CONFIGS.TOPICS.split(",").filter(Boolean);
    const combinedSet = new Set([
      ...configuredTopics,
      ...seenTopics,
      ...selectedTopics.filter((t) => t !== "*"),
    ]);

    return Array.from(combinedSet).map((topic) => {
      const separator = findFirstSeparator(topic);
      return {
        id: topic,
        category: separator ? topic.split(separator)[0] : topic,
      };
    });
  }, [selectedTopics, seenTopics]);

  const [searchQuery, setSearchQuery] = useState("");
  const [expandedCategories, setExpandedCategories] = useState<string[]>(
    Array.from(new Set(allTopics.map((topic) => topic.category))),
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
    const filtered = allTopics.filter((topic) =>
      topic.id.toLowerCase().includes(searchQuery.toLowerCase()),
    );

    return filtered.reduce(
      (acc, topic) => {
        const category = topic.category;
        if (!acc[category]) {
          acc[category] = [];
        }
        acc[category].push(topic);
        return acc;
      },
      {} as Record<string, Topic[]>,
    );
  }, [allTopics, searchQuery]);

  const toggleCategory = (category: string) => {
    setExpandedCategories((prev) =>
      prev.includes(category)
        ? prev.filter((c) => c !== category)
        : [...prev, category],
    );
  };

  const toggleTopic = (topicId: string) => {
    if (isEverythingSelected) {
      selectedTopics = [];
    }

    // Ensure if we manually add/toggle a custom topic it's immediately tracked
    setSeenTopics((prev) => Array.from(new Set([...prev, topicId])));

    const newSelected = selectedTopics.includes(topicId)
      ? selectedTopics.filter((id) => id !== topicId)
      : [...selectedTopics, topicId];
    onTopicsChange(newSelected);
  };

  const exactMatchExists = allTopics.some(
    (t) => t.id.toLowerCase() === searchQuery.toLowerCase(),
  );

  // The backend glob matcher (matchTopicPattern) natively supports '*' anywhere in the topic string.
  const isWildcardSearch = searchQuery.includes("*");

  const showAddTopic =
    searchQuery.length > 0 && !exactMatchExists && isWildcardSearch;

  return (
    <div className="topic-picker" style={{ maxHeight: maxHeight }}>
      <div className="topic-picker__header">
        <SearchInput
          value={searchQuery}
          onChange={(value) => setSearchQuery(value)}
          placeholder="Filter or type a wildcard topic (e.g., order.*)"
        />
      </div>
      <div className="topic-picker__content">
        {searchQuery.length === 0 && (
          <div className="topic-picker__select-all">
            <Checkbox
              label="Select All (*)"
              checked={isEverythingSelected}
              onChange={toggleSelectAll}
            />
          </div>
        )}
        {showAddTopic && (
          <div className="topic-picker__flat-topic">
            <Checkbox
              checked={false}
              onChange={() => {
                toggleTopic(searchQuery);
                setSearchQuery("");
              }}
              label={`Add "${searchQuery}"`}
              monospace
              disabled={isEverythingSelected}
            />
          </div>
        )}
        {Object.entries(categorizedTopics).length === 0 && !showAddTopic && (
          <span className="body-m muted">No topics match your filter.</span>
        )}
        {Object.entries(categorizedTopics).map(([category, categoryTopics]) => {
          const isExpanded = expandedCategories.includes(category);

          const firstNormalTopic = categoryTopics.find(
            (t) => !t.id.endsWith("*") && t.id !== category,
          );
          const separator = firstNormalTopic
            ? findFirstSeparator(firstNormalTopic.id)
            : ".";
          const wildcardId = `${category}${separator || "."}*`;

          const hasWildcard = selectedTopics.includes(wildcardId);
          const visibleTopics = categoryTopics.filter(
            (t) => t.id !== wildcardId,
          );

          const selectedCount = visibleTopics.filter((topic) =>
            selectedTopics.includes(topic.id),
          ).length;
          // Always show indeterminate if any children are selected but the wildcard itself is not.
          const isIndeterminate = !hasWildcard && selectedCount > 0;

          // Check if this is a flat topic (no actual nesting)
          const isFlatTopic =
            (categoryTopics.length === 1 &&
              categoryTopics[0].id === category) ||
            (categoryTopics.length === 1 &&
              categoryTopics[0].id === wildcardId);

          if (isFlatTopic) {
            const topic = categoryTopics[0];
            return (
              <div key={category} className="topic-picker__flat-topic">
                <Checkbox
                  checked={
                    isEverythingSelected || selectedTopics.includes(topic.id)
                  }
                  onChange={() => toggleTopic(topic.id)}
                  label={topic.id}
                  monospace
                  disabled={isEverythingSelected}
                />
              </div>
            );
          }

          return (
            <div key={category} className="topic-picker__category">
              <div className="topic-picker__category-header">
                {visibleTopics.length > 0 ? (
                  <button
                    type="button"
                    onClick={() => toggleCategory(category)}
                    className="topic-picker__category-toggle"
                  >
                    <span className={`arrow ${isExpanded ? "expanded" : ""}`}>
                      <DropdownIcon />
                    </span>
                  </button>
                ) : (
                  <span style={{ width: 24, display: "inline-block" }} />
                )}
                <Checkbox
                  label={`${category} ${
                    hasWildcard || isEverythingSelected ? `(${wildcardId})` : ""
                  }`.trim()}
                  checked={isEverythingSelected || hasWildcard}
                  indeterminate={!isEverythingSelected && isIndeterminate}
                  onChange={() => {
                    if (isEverythingSelected) {
                      selectedTopics = [];
                    }
                    if (hasWildcard) {
                      onTopicsChange(
                        selectedTopics.filter((id) => id !== wildcardId),
                      );
                    } else {
                      // Clicking the parent immediately subscribes to the wildcard
                      // and clears out any individual child selections for a clean state.
                      const childrenIds = categoryTopics.map((t) => t.id);
                      const newSelected = selectedTopics.filter(
                        (id) => !childrenIds.includes(id),
                      );
                      newSelected.push(wildcardId);
                      onTopicsChange(newSelected);
                    }
                  }}
                  disabled={isEverythingSelected}
                />
              </div>
              {isExpanded && visibleTopics.length > 0 && (
                <div className="topic-picker__topics">
                  {visibleTopics.map((topic) => (
                    <div key={topic.id} className="topic-picker__topic">
                      <Checkbox
                        checked={
                          isEverythingSelected ||
                          hasWildcard ||
                          selectedTopics.includes(topic.id)
                        }
                        onChange={() => toggleTopic(topic.id)}
                        label={topic.id}
                        monospace
                        disabled={isEverythingSelected || hasWildcard}
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
