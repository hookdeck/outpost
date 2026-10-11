import { CopyIcon } from "../Icons";
import { showToast } from "../Toast/Toast";

import "./CopyButton.scss";

interface CopyButtonProps {
  value: string;
}

const copyUsingFallback = (value: string) => {
  const textarea = document.createElement("textarea");
  textarea.value = value;
  textarea.style.position = "fixed"; // Avoid scrolling to bottom
  textarea.style.opacity = "0";
  document.body.appendChild(textarea);
  textarea.select();

  try {
    document.execCommand("copy");
  } catch (err) {
    console.warn("Copy to clipboard failed:", err);
  }

  document.body.removeChild(textarea);
};

const handleCopy = (value: string) => {
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(value).catch(() => {
      // Fallback if permission is denied or other errors
      copyUsingFallback(value);
    });
  } else {
    copyUsingFallback(value);
  }
  showToast("success", "Copied to clipboard.");
};

export const CopyButton = ({ value }: CopyButtonProps) => {
  return (
    <button
      type="button"
      onClick={() => handleCopy(value)}
      className="unstyled-button"
      aria-label="Copy to clipboard"
    >
      <CopyIcon />
    </button>
  );
};
