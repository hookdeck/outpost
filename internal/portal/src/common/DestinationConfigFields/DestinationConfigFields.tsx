import { useState, useRef, useEffect } from "react";
import {
  Destination,
  DestinationTypeReference,
} from "../../typings/Destination";
import "./DestinationConfigFields.scss";
import { EditIcon, HelpIcon } from "../Icons";
import Tooltip from "../Tooltip/Tooltip";
import Button from "../Button/Button";
import ConfigurationModal from "../ConfigurationModal/ConfigurationModal";
import { Checkbox } from "../Checkbox/Checkbox";

const DestinationConfigFields = ({
  destination,
  type,
}: {
  destination?: Destination;
  type: DestinationTypeReference;
}) => {
  const [unlockedFields, setUnlockedFields] = useState<Record<string, boolean>>(
    {}
  );
  const [lastUnlockedField, setLastUnlockedField] = useState<string | null>(
    null
  );
  const [showConfigModal, setShowConfigModal] = useState(false);

  const inputRefs = useRef<Record<string, HTMLInputElement>>({});

  const unlockField = (key: string) => {
    setUnlockedFields((prev) => ({
      ...prev,
      [key]: !prev[key],
    }));
    setLastUnlockedField(key);

    if (inputRefs.current[key]) {
      inputRefs.current[key].value = "";
    }
  };

  useEffect(() => {
    if (lastUnlockedField && inputRefs.current[lastUnlockedField]) {
      inputRefs.current[lastUnlockedField].focus();
    }
  }, [lastUnlockedField]);

  console.log(type);
  console.log(destination);
  console.log(unlockedFields);

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
                    ? unlockedFields[field.key]
                      ? ""
                      : destination?.credentials[field.key] || field.default
                    : destination?.config[field.key] || destination?.credentials[field.key] || field.default
                }
                disabled={
                  "sensitive" in field && field.sensitive
                    ? destination?.credentials[field.key] &&
                      !unlockedFields[field.key]
                    : field.disabled
                }
                required={field.required}
                minLength={field.minlength}
                maxLength={field.maxlength}
                pattern={field.pattern}
              />
              {"sensitive" in field &&
              field.sensitive &&
              destination?.credentials[field.key] &&
              !unlockedFields[field.key] ? (
                <button type="button" onClick={() => unlockField(field.key)}>
                  <Tooltip content="Edit secret value" align="end">
                    <EditIcon />
                  </Tooltip>
                </button>
              ) : null}
            </div>
          )}
          {field.type === "checkbox" && (
            <Checkbox
              label=""
              id={field.key}
              name={field.key}
              defaultChecked={
                destination?.config[field.key] ??
                destination?.credentials[field.key] ??
                field.default == "on" ??
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
