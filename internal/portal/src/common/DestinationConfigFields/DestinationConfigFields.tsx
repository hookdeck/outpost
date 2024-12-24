import {
  Destination,
  DestinationTypeReference,
} from "../../typings/Destination";
import "./DestinationConfigFields.scss";

const DestinationConfigFields = ({
  destination,
  type,
}: {
  destination: Destination;
  type: DestinationTypeReference;
}) => {
  return (
    <>
      {[...type.config_fields, ...type.credential_fields].map((field) => (
        <div key={field.key} className="destination-config-field">
          <label htmlFor={field.key}>
            {field.label}
            {field.required && <span className="required">*</span>}
          </label>
          {field.type === "text" && (
            <input
              type={
                "sensitive" in field && field.sensitive ? "password" : "text"
              }
              id={field.key}
              name={field.key}
              defaultValue={
                "sensitive" in field && field.sensitive && !field.disabled
                  ? ""
                  : destination.config[field.key] ||
                    destination.credentials[field.key] ||
                    ""
              }
              disabled={field.disabled}
              required={field.required}
              minLength={field.minlength}
              maxLength={field.maxlength}
              pattern={field.pattern}
            />
          )}
          {field.type === "checkbox" && (
            <input
              type="checkbox"
              id={field.key}
              name={field.key}
              defaultChecked={
                destination.config[field.key] ??
                destination.credentials[field.key] ??
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
    </>
  );
};

export default DestinationConfigFields;
