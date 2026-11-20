import { useState, useRef, useEffect } from "react";
import type {
  Destination,
  DestinationTypeReference,
} from "../../typings/Destination";
import "./DestinationConfigFields.scss";
import { EditIcon, HelpIcon, CloseIcon } from "../Icons";
import Tooltip from "../Tooltip/Tooltip";
import Button from "../Button/Button";
import { useSidebar } from "../Sidebar/Sidebar";
import { ConfigurationGuide } from "../ConfigurationGuide/ConfigurationGuide";
import { Checkbox } from "../Checkbox/Checkbox";
import KeyValueMapField from "../KeyValueMapField/KeyValueMapField";
import {
  CREDENTIAL_FIELD_PREFIX,
  isCheckedValue,
} from "../../utils/formHelper";
import CONFIGS from "../../config";

const DestinationConfigFields = ({
  destination,
  type,
  onChange,
}: {
  destination?: Destination;
  type: DestinationTypeReference;
  onChange?: () => void;
}) => {
  const [unlockedSensitiveFields, setUnlockedSensitiveFields] = useState<
    Record<string, boolean>
  >({});
  const [lastUnlockedSensitiveField, setLastUnlockedSensitiveField] = useState<
    string | null
  >(null);
  const sidebar = useSidebar();

  const inputRefs = useRef<Record<string, HTMLInputElement>>({});

  const unlockSensitiveField = (key: string) => {
    setUnlockedSensitiveFields((prev) => ({
      ...prev,
      [key]: !prev[key],
    }));
    setLastUnlockedSensitiveField(key);

    if (inputRefs.current[key]) {
      inputRefs.current[key].value = "";
    }
  };

  const lockSensitiveField = (key: string) => {
    setUnlockedSensitiveFields((prev) => ({
      ...prev,
      [key]: false,
    }));
    setLastUnlockedSensitiveField(null);
    // Restore the original value when locking/canceling
    if (inputRefs.current[key]) {
      inputRefs.current[key].value = destination?.credentials[key] || ""; // Restore original or empty
    }
  };

  useEffect(() => {
    if (
      lastUnlockedSensitiveField &&
      inputRefs.current[lastUnlockedSensitiveField]
    ) {
      inputRefs.current[lastUnlockedSensitiveField].focus();
    }
  }, [lastUnlockedSensitiveField]);

  return (
    <>
      {type.instructions && (
        <Button
          onClick={() =>
            sidebar.toggle("configuration", <ConfigurationGuide type={type} />)
          }
          className="config-guide-button"
        >
          <HelpIcon />
          Configuration Guide
        </Button>
      )}
      {type.setup_link && (
        <div className="destination-setup-link config-guide-button">
          <a
            href={type.setup_link.href}
            target="_blank"
            rel="noopener noreferrer"
            className="button"
          >
            {type.setup_link.cta}
          </a>
        </div>
      )}
      {[
        ...type.config_fields.map((field) => ({
          field,
          name: field.key,
          saved: destination?.config,
        })),
        ...type.credential_fields.map((field) => ({
          field,
          name: CREDENTIAL_FIELD_PREFIX + field.key,
          saved: destination?.credentials,
        })),
      ]
        .filter(({ field }) => {
          // Filter out header fields if the feature flag is not enabled
          if (
            field.key === "custom_headers" &&
            CONFIGS.ENABLE_WEBHOOK_CUSTOM_HEADERS !== "true"
          ) {
            return false;
          }
          return true;
        })
        .map(({ field, name, saved }) => (
          <div key={name} className="destination-config-field">
            <label htmlFor={name}>
              {field.label}
              {field.required && <span className="required">*</span>}
            </label>
            {field.type === "text" && (
              <div className="input-container">
                <input
                  ref={(el) => {
                    if (el) inputRefs.current[field.key] = el;
                  }}
                  type={
                    "sensitive" in field && field.sensitive
                      ? "password"
                      : "text"
                  }
                  placeholder={""}
                  id={name}
                  name={name}
                  defaultValue={
                    "sensitive" in field && field.sensitive
                      ? unlockedSensitiveFields[field.key]
                        ? ""
                        : saved?.[field.key] || field.default
                      : saved?.[field.key] || field.default
                  }
                  disabled={
                    "sensitive" in field && field.sensitive
                      ? destination?.credentials[field.key] &&
                        !unlockedSensitiveFields[field.key]
                      : field.disabled
                  }
                  required={field.required}
                  minLength={field.minlength}
                  maxLength={field.maxlength}
                  pattern={field.pattern}
                />
                {/* Show Edit button if sensitive and locked */}
                {"sensitive" in field &&
                  field.sensitive &&
                  destination?.credentials[field.key] &&
                  !unlockedSensitiveFields[field.key] && (
                    <button
                      type="button"
                      onClick={() => unlockSensitiveField(field.key)}
                    >
                      <Tooltip content="Edit secret value" align="end">
                        <EditIcon />
                      </Tooltip>
                    </button>
                  )}

                {/* Show Cancel button if sensitive and unlocked */}
                {"sensitive" in field &&
                  field.sensitive &&
                  unlockedSensitiveFields[field.key] && (
                    <button
                      type="button"
                      onClick={() => lockSensitiveField(field.key)}
                    >
                      <Tooltip content="Cancel editing" align="end">
                        <CloseIcon />
                      </Tooltip>
                    </button>
                  )}
              </div>
            )}
            {field.type === "select" && (
              <select
                id={name}
                name={name}
                defaultValue={saved?.[field.key] || field.default || ""}
                disabled={field.disabled}
                required={field.required}
                onChange={onChange}
              >
                {field.options?.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
            )}
            {field.type === "checkbox" && (
              <Checkbox
                label=""
                id={name}
                name={name}
                defaultChecked={
                  (saved?.[field.key] !== undefined
                    ? isCheckedValue(saved?.[field.key])
                    : undefined) ??
                  (field.default !== undefined
                    ? isCheckedValue(field.default)
                    : undefined) ??
                  false
                }
                disabled={field.disabled}
                required={field.required}
              />
            )}
            {field.description && (
              <p
                className="description"
                style={{
                  marginBottom:
                    field.type === "key_value_map"
                      ? "var(--spacing-2)"
                      : "var(--spacing-4)",
                }}
              >
                {field.description}
              </p>
            )}
            {field.type === "key_value_map" && (
              <KeyValueMapField
                {...keyValueMapProps(field, saved?.[field.key])}
                name={name}
                disabled={field.disabled}
                keyPlaceholder={field.key_placeholder}
                valuePlaceholder={field.value_placeholder}
                onChange={onChange}
              />
            )}
          </div>
        ))}
    </>
  );
};

// Saved values of a sensitive map never reach the portal, only their masked
// form, which is shown as the placeholder and never put in the input. On
// save, a row left empty is left out of the update, which keeps its saved
// value (see keyValueMapUpdate).
function keyValueMapProps(
  field: DestinationTypeReference["credential_fields"][number],
  saved: string | undefined,
) {
  const sensitive = "sensitive" in field && field.sensitive === true;
  if (sensitive && saved) {
    return {
      sensitive,
      defaultValue: blankValues(saved),
      maskedValues: Object.fromEntries(parseEntries(saved)),
    };
  }
  return { sensitive, defaultValue: saved || field.default };
}

function parseEntries(value: string): [string, string][] {
  try {
    const parsed = JSON.parse(value);
    if (typeof parsed === "object" && parsed !== null) {
      return Object.entries(parsed).map(([k, v]) => [k, String(v)]);
    }
  } catch {
    // Not valid JSON
  }
  return [];
}

function blankValues(value: string): string {
  return JSON.stringify(
    Object.fromEntries(parseEntries(value).map(([name]) => [name, ""])),
  );
}

export default DestinationConfigFields;
