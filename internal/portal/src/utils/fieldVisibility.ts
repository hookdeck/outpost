import type {
  ConfigField,
  DestinationTypeReference,
} from "../typings/Destination";

type Values = Record<string, unknown>;

const allFields = (type: DestinationTypeReference): ConfigField[] => [
  ...type.config_fields,
  ...type.credential_fields,
];

/**
 * Keys of the fields that other fields' visibility depends on.
 */
export function controllerKeys(type: DestinationTypeReference): string[] {
  const keys = allFields(type)
    .map((field) => field.visible_when?.key)
    .filter((key): key is string => !!key);
  return [...new Set(keys)];
}

/**
 * Value of the field key in values, falling back to the field's default.
 */
export function fieldValue(
  type: DestinationTypeReference,
  values: Values,
  key: string,
): string {
  const value = values[key];
  if (value !== undefined && value !== null && value !== "") {
    return String(value);
  }
  return allFields(type).find((field) => field.key === key)?.default ?? "";
}

/**
 * Whether field applies given the current values of the fields it depends on.
 */
export function isFieldVisible(
  type: DestinationTypeReference,
  field: ConfigField,
  values: Values,
): boolean {
  if (!field.visible_when) {
    return true;
  }
  return field.visible_when.values.includes(
    fieldValue(type, values, field.visible_when.key),
  );
}
