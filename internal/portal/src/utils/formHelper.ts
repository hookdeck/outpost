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
