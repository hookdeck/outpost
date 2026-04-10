import { useState, useEffect, useRef } from "react";
import type { Filter } from "../../typings/Destination";
import "./FilterField.scss";

interface FilterFieldProps {
  value?: Filter;
  onChange: (filter: Filter) => void;
  onValidChange?: (isValid: boolean) => void;
  disabled?: boolean;
}

const FilterField = ({
  value,
  onChange,
  onValidChange,
  disabled,
}: FilterFieldProps) => {
  const [jsonText, setJsonText] = useState(() => {
    if (value && Object.keys(value).length > 0) {
      return JSON.stringify(value, null, 2);
    }
    return "";
  });
  const [error, setError] = useState<string | null>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  // Update text when value prop changes externally
  useEffect(() => {
    if (value && Object.keys(value).length > 0) {
      const newText = JSON.stringify(value, null, 2);
      if (newText !== jsonText) {
        setJsonText(newText);
        setError(null);
      }
    }
  }, [value]);

  // Notify parent of validity changes
  useEffect(() => {
    if (onValidChange) {
      onValidChange(error === null);
    }
  }, [error, onValidChange]);

  const handleTextChange = (text: string) => {
    setJsonText(text);

    if (text.trim() === "") {
      setError(null);
      onChange(null);
      return;
    }

    try {
      const parsed = JSON.parse(text);
      if (typeof parsed !== "object" || Array.isArray(parsed)) {
        setError("Filter must be a JSON object");
        return;
      }
      setError(null);
      onChange(parsed);
    } catch (e) {
      setError("Invalid JSON");
    }
  };

  // Auto-resize textarea
  useEffect(() => {
    if (textareaRef.current) {
      textareaRef.current.style.height = "auto";
      textareaRef.current.style.height = `${Math.max(
        120,
        textareaRef.current.scrollHeight,
      )}px`;
    }
  }, [jsonText]);

  return (
    <div className={`filter-field ${error ? "filter-field--error" : ""}`}>
      <textarea
        ref={textareaRef}
        value={jsonText}
        onChange={(e) => handleTextChange(e.target.value)}
        placeholder={`{
  "data": {
    "type": { "$in": ["order.created", "order.updated"] },
    "amount": { "$gte": 100 }
  }
}`}
        disabled={disabled}
        spellCheck={false}
        className="filter-field__textarea"
      />
      {error && <p className="filter-field__error">{error}</p>}
    </div>
  );
};

export default FilterField;
