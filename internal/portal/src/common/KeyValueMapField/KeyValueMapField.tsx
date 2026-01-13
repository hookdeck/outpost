import { useState, useEffect, useRef } from "react";
import { CloseIcon, PlusIcon } from "../Icons";
import "./KeyValueMapField.scss";

interface KeyValuePair {
  key: string;
  value: string;
}

interface KeyValueMapFieldProps {
  name: string;
  defaultValue?: string;
  disabled?: boolean;
  keyPlaceholder?: string;
  valuePlaceholder?: string;
  onChange?: (value: string) => void;
}

const KeyValueMapField: React.FC<KeyValueMapFieldProps> = ({
  name,
  defaultValue,
  disabled,
  keyPlaceholder = "Key",
  valuePlaceholder = "Value",
  onChange,
}) => {
  const [pairs, setPairs] = useState<KeyValuePair[]>(() => {
    if (defaultValue) {
      try {
        const parsed = JSON.parse(defaultValue);
        if (typeof parsed === "object" && parsed !== null) {
          const entries = Object.entries(parsed);
          if (entries.length > 0) {
            return entries.map(([key, value]) => ({
              key,
              value: String(value),
            }));
          }
        }
      } catch {
        // Invalid JSON, start with empty
      }
    }
    return [];
  });

  const serializedValue = JSON.stringify(
    pairs.reduce(
      (acc, pair) => {
        if (pair.key.trim()) {
          acc[pair.key.trim()] = pair.value;
        }
        return acc;
      },
      {} as Record<string, string>
    )
  );

  // Track previous value to detect actual changes (not initial render)
  const prevSerializedValueRef = useRef<string | null>(null);

  // Call onChange callback only when serialized value actually changes
  useEffect(() => {
    // On first render, just store the initial value without calling onChange
    if (prevSerializedValueRef.current === null) {
      prevSerializedValueRef.current = serializedValue;
      return;
    }

    // Only call onChange if value actually changed
    if (prevSerializedValueRef.current !== serializedValue) {
      prevSerializedValueRef.current = serializedValue;
      onChange?.(serializedValue);
    }
  }, [serializedValue]);

  const updatePair = (
    index: number,
    field: "key" | "value",
    newValue: string
  ) => {
    setPairs((prev) =>
      prev.map((pair, i) =>
        i === index ? { ...pair, [field]: newValue } : pair
      )
    );
  };

  const removePair = (index: number) => {
    setPairs((prev) => prev.filter((_, i) => i !== index));
  };

  const addPair = () => {
    setPairs((prev) => [...prev, { key: "", value: "" }]);
  };

  return (
    <div className="key-value-map-field">
      <input type="hidden" name={name} value={serializedValue} />
      {pairs.length > 0 && (
        <div className="key-value-map-field__rows">
          {pairs.map((pair, index) => (
            <div key={index} className="key-value-map-field__row">
              <input
                type="text"
                placeholder={keyPlaceholder}
                value={pair.key}
                onChange={(e) => updatePair(index, "key", e.target.value)}
                disabled={disabled}
                className="key-value-map-field__key"
              />
              <input
                type="text"
                placeholder={valuePlaceholder}
                value={pair.value}
                onChange={(e) => updatePair(index, "value", e.target.value)}
                disabled={disabled}
                className="key-value-map-field__value"
              />
              <button
                type="button"
                onClick={() => removePair(index)}
                disabled={disabled}
                className="button button__minimal key-value-map-field__remove"
                aria-label="Remove row"
              >
                <CloseIcon />
              </button>
            </div>
          ))}
        </div>
      )}
      <button
        type="button"
        onClick={addPair}
        disabled={disabled}
        className="button key-value-map-field__add"
      >
        <PlusIcon /> Add header
      </button>
    </div>
  );
};

export default KeyValueMapField;
