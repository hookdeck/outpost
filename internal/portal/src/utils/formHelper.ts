/**
 * Extracts form data into an object, ensuring checkboxes are represented as booleans.
 *
 * @param form The HTMLFormElement to extract data from.
 * @returns An object containing form data, with checkbox values as true/false.
 */
export function getFormValues(form: HTMLFormElement): Record<string, any> {
  const formData = new FormData(form);
  const values: Record<string, any> = Object.fromEntries(formData.entries());

  // Explicitly handle checkboxes to ensure boolean true/false values
  form.querySelectorAll('input[type="checkbox"]').forEach((checkbox) => {
    const cb = checkbox as HTMLInputElement;
    // FormData only includes checked boxes, potentially with value "on".
    // We need to ensure all checkboxes are present with true/false.
    values[cb.name] = cb.checked;
  });

  return values;
}

/**
 * Determines if a string value represents a checked state in a form.
 *
 * @param {string} value - The string value to be evaluated
 * @returns {boolean} - Returns true if the value represents a checked state ("true" or "on"), false otherwise
 *
 * @example
 * // Returns true
 * isCheckedValue("true");
 * isCheckedValue("on");
 *
 * @example
 * // Returns false
 * isCheckedValue("false");
 * isCheckedValue("");
 */
export function isCheckedValue(value: string | undefined): boolean {
  return value === "true" || value === "on";
}

// Form input names of credential fields carry this prefix, so a config and a
// credential field can share a key (webhook custom_headers).
export const CREDENTIAL_FIELD_PREFIX = "credentials.";

type FieldRef = { key: string; type: string; sensitive?: boolean };

/**
 * Picks the values of the given fields out of the form values, keyed by field
 * key. Credential fields are read from their prefixed input names.
 */
export function pickFieldValues(
  values: Record<string, any>,
  fields: FieldRef[],
  prefix = "",
): Record<string, string> {
  const picked: Record<string, string> = {};
  for (const field of fields) {
    const name = prefix + field.key;
    if (name in values) {
      picked[field.key] = String(values[name]);
    }
  }
  return picked;
}

/**
 * Turns the edited rows of a key/value map field into an update for the API,
 * which merges maps key by key: keys left out keep their saved value, and a
 * null value removes a key. Keys match case-insensitively, as on the API.
 * Returns undefined when nothing changed and null when every key was removed.
 *
 * Saved values of a sensitive field never reach the portal, so a row with an
 * empty value under a saved key means "unchanged" and is left out.
 */
export function keyValueMapUpdate(
  saved: string | undefined,
  edited: string,
  sensitive: boolean,
): string | null | undefined {
  const savedMap = parseStringMap(saved);
  const editedMap = parseStringMap(edited);
  if (Object.keys(editedMap).length === 0) {
    return Object.keys(savedMap).length === 0 ? undefined : null;
  }
  const savedKey = (key: string) =>
    Object.keys(savedMap).find((k) => k.toLowerCase() === key.toLowerCase());
  const editedKeys = new Set(
    Object.keys(editedMap).map((k) => k.toLowerCase()),
  );
  const update: Record<string, string | null> = {};
  for (const key of Object.keys(savedMap)) {
    if (!editedKeys.has(key.toLowerCase())) {
      update[key] = null;
    }
  }
  for (const [key, value] of Object.entries(editedMap)) {
    const match = savedKey(key);
    if (match !== undefined) {
      if (sensitive && value === "") {
        continue;
      }
      if (!sensitive && match === key && value === savedMap[key]) {
        continue;
      }
    }
    update[key] = value;
  }
  return Object.keys(update).length === 0 ? undefined : JSON.stringify(update);
}

function parseStringMap(value: string | undefined): Record<string, string> {
  if (!value) {
    return {};
  }
  try {
    const parsed = JSON.parse(value);
    if (
      typeof parsed === "object" &&
      parsed !== null &&
      !Array.isArray(parsed)
    ) {
      return Object.fromEntries(
        Object.entries(parsed).map(([k, v]) => [k, String(v)]),
      );
    }
  } catch {
    // Not valid JSON
  }
  return {};
}
