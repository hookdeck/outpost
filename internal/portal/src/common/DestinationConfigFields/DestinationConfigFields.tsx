import { useState, useRef, useEffect } from "react";
import {
  Destination,
  DestinationTypeReference,
} from "../../typings/Destination";
import "./DestinationConfigFields.scss";
import { EditIcon, HelpIcon, CloseIcon } from "../Icons";
import Tooltip from "../Tooltip/Tooltip";
import Button from "../Button/Button";
import ConfigurationModal from "../ConfigurationModal/ConfigurationModal";
import { Checkbox } from "../Checkbox/Checkbox";
import { isCheckedValue } from "../../utils/formHelper";

const DestinationConfigFields = ({
  destination,
  type,
}: {
  destination?: Destination;
  type: DestinationTypeReference;
}) => {
  const [unlockedSensitiveFields, setUnlockedSensitiveFields] = useState<
    Record<string, boolean>
  >({});
  const [lastUnlockedSensitiveField, setLastUnlockedSensitiveField] = useState<
    string | null
  >(null);
  const [showConfigModal, setShowConfigModal] = useState(false);

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
          onClick={() => setShowConfigModal(!showConfigModal)}
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
      {[...type.config_fields, ...type.credential_fields].map((field) => (
        <div key={field.key} className="destination-config-field">
          <label htmlFor={field.key}>
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
                  "sensitive" in field && field.sensitive ? "password" : "text"
                }
                placeholder={""}
                id={field.key}
                name={field.key}
                defaultValue={
                  "sensitive" in field && field.sensitive
                    ? unlockedSensitiveFields[field.key]
                      ? ""
                      : destination?.credentials[field.key] || field.default
                    : destination?.config[field.key] ||
                      destination?.credentials[field.key] ||
                      field.default
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
                  <button type="button" onClick={() => unlockSensitiveField(field.key)}>
                    <Tooltip content="Edit secret value" align="end">
                      <EditIcon />
                    </Tooltip>
                  </button>
                )}

              {/* Show Cancel button if sensitive and unlocked */}
              {"sensitive" in field &&
                field.sensitive &&
                unlockedSensitiveFields[field.key] && (
                  <button type="button" onClick={() => lockSensitiveField(field.key)}>
                    <Tooltip content="Cancel editing" align="end">
                      <CloseIcon />
                    </Tooltip>
                  </button>
                )}
            </div>
          )}
          {field.type === "checkbox" && (
            <Checkbox
              label=""
              id={field.key}
              name={field.key}
              defaultChecked={
                (destination?.config[field.key] !== undefined
                  ? isCheckedValue(destination?.config[field.key])
                  : undefined) ??
                (destination?.credentials[field.key] !== undefined
                  ? isCheckedValue(destination?.credentials[field.key])
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
            <p className="description">{field.description}</p>
          )}
        </div>
      ))}

      {showConfigModal && (
        <ConfigurationModal
          type={type}
          onClose={() => setShowConfigModal(false)}
        />
      )}
    </>
  );
};

export default DestinationConfigFields;
