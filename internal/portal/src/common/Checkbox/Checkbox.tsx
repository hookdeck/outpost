import { useEffect, useRef } from "react";
import "./Checkbox.scss";

interface CheckboxProps extends Omit<
  React.InputHTMLAttributes<HTMLInputElement>,
  "type"
> {
  label: string;
  indeterminate?: boolean;
  monospace?: boolean;
}

export const Checkbox: React.FC<CheckboxProps> = ({
  label,
  monospace,
  indeterminate,
  ...props
}) => {
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (inputRef.current) {
      inputRef.current.indeterminate = !!indeterminate;
    }
  }, [indeterminate]);

  return (
    <label className="checkbox">
      <div className="checkbox__input-wrapper">
        <input
          type="checkbox"
          className="checkbox__input"
          ref={inputRef}
          {...props}
        />
        <span
          className={`checkbox__checkmark ${
            indeterminate ? "indeterminate" : ""
          } `}
        />
      </div>
      {label && (
        <span className={`checkbox__label ${monospace ? "monospace" : ""}`}>
          {label}
        </span>
      )}
    </label>
  );
};
