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
  const allowWildcardTopics = CONFIGS.TOPICS_ALLOW_WILDCARDS === "true";

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

  const toggleCategorySelection = (topicsInCategory: Topic[]) => {
    const currentTopics = isEverythingSelected ? [] : selectedTopics;
    const categoryTopicIds = topicsInCategory.map((t) => t.id);
    const areAllSelected = categoryTopicIds.every((id) =>
      currentTopics.includes(id),
    );

    if (areAllSelected) {
      onTopicsChange(
        currentTopics.filter((id) => !categoryTopicIds.includes(id)),
      );
    } else {
      const newSelected = new Set([...currentTopics, ...categoryTopicIds]);
      onTopicsChange(Array.from(newSelected));
    }
  };

  const toggleTopic = (topicId: string) => {
    const currentTopics = isEverythingSelected ? [] : selectedTopics;

    // Ensure if we manually add/toggle a custom topic it's immediately tracked
    setSeenTopics((prev) => Array.from(new Set([...prev, topicId])));

    const newSelected = currentTopics.includes(topicId)
      ? currentTopics.filter((id) => id !== topicId)
      : [...currentTopics, topicId];
    onTopicsChange(newSelected);
  };

  const exactMatchExists = allTopics.some(
    (t) => t.id.toLowerCase() === searchQuery.toLowerCase(),
  );

  // The backend glob matcher supports '*' anywhere in the topic string when enabled.
  const isWildcardSearch = searchQuery.includes("*");

  const showAddTopic =
    allowWildcardTopics &&
    searchQuery.length > 0 &&
    !exactMatchExists &&
    isWildcardSearch;

  return (
    <div className="topic-picker" style={{ maxHeight: maxHeight }}>
      <div className="topic-picker__header">
        <SearchInput
          value={searchQuery}
          onChange={(value) => setSearchQuery(value)}
          placeholder={
            allowWildcardTopics
              ? "Filter or type a wildcard topic (e.g., order.*)"
              : "Filter topics..."
          }
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

          const hasWildcard =
            allowWildcardTopics && selectedTopics.includes(wildcardId);
          const visibleTopics = allowWildcardTopics
            ? categoryTopics.filter((t) => t.id !== wildcardId)
            : categoryTopics;

          const selectedCount = visibleTopics.filter((topic) =>
            selectedTopics.includes(topic.id),
          ).length;
          const groupSelectionLabel = allowWildcardTopics
            ? hasWildcard || isEverythingSelected
              ? "Selected and future topics"
              : selectedCount > 0
                ? "Selected topics only"
                : ""
            : "";
          const areAllSelected =
            visibleTopics.length > 0 && selectedCount === visibleTopics.length;
          const isIndeterminate = allowWildcardTopics
            ? !hasWildcard && selectedCount > 0
            : selectedCount > 0 && !areAllSelected;

          // Check if this is a flat topic (no actual nesting)
          const isFlatTopic =
            (categoryTopics.length === 1 &&
              categoryTopics[0].id === category) ||
            (allowWildcardTopics &&
              categoryTopics.length === 1 &&
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
                  label={category}
                  checked={
                    isEverythingSelected ||
                    (allowWildcardTopics ? hasWildcard : areAllSelected)
                  }
                  indeterminate={!isEverythingSelected && isIndeterminate}
                  onChange={() => {
                    if (!allowWildcardTopics) {
                      toggleCategorySelection(visibleTopics);
                      return;
                    }

                    const currentTopics = isEverythingSelected
                      ? []
                      : selectedTopics;

                    if (hasWildcard) {
                      onTopicsChange(
                        currentTopics.filter((id) => id !== wildcardId),
                      );
                    } else {
                      // Clicking the parent immediately subscribes to the wildcard
                      // and clears out any individual child selections for a clean state.
                      const childrenIds = categoryTopics.map((t) => t.id);
                      const newSelected = currentTopics.filter(
                        (id) => !childrenIds.includes(id),
                      );
                      newSelected.push(wildcardId);
                      onTopicsChange(newSelected);
                    }
                  }}
                  disabled={isEverythingSelected}
                />
                {groupSelectionLabel && (
                  <span className="topic-picker__category-selection">
                    {groupSelectionLabel}
                  </span>
                )}
              </div>
              {isExpanded && visibleTopics.length > 0 && (
                <div className="topic-picker__topics">
                  {visibleTopics.map((topic) => (
                    <div key={topic.id} className="topic-picker__topic">
                      <Checkbox
                        checked={
                          isEverythingSelected ||
                          (allowWildcardTopics && hasWildcard) ||
                          selectedTopics.includes(topic.id)
                        }
                        onChange={() => {
                          if (allowWildcardTopics && hasWildcard) {
                            const explicitTopics = visibleTopics
                              .map((t) => t.id)
                              .filter((id) => id !== topic.id);
                            onTopicsChange([
                              ...selectedTopics.filter(
                                (id) => id !== wildcardId,
                              ),
                              ...explicitTopics,
                            ]);
                            return;
                          }

                          toggleTopic(topic.id);
                        }}
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
