import { Link } from "react-router-dom";
import { FC, PropsWithChildren } from "react";
import "./Button.scss";
import { Loading } from "../Icons";

interface ButtonProps {
  to?: string;
  onClick?: (e: React.MouseEvent<HTMLButtonElement>) => void;
  disabled?: boolean;
  type?: "button" | "submit" | "reset";
  className?: string;
  loading?: boolean;
  primary?: boolean;
  danger?: boolean;
  minimal?: boolean;
  icon?: boolean;
  iconLabel?: string;
}

const Button: FC<PropsWithChildren<ButtonProps>> = ({
  primary = false,
  to,
  onClick,
  disabled = false,
  type = "button",
  children,
  className,
  loading = false,
  danger = false,
  minimal = false,
  icon,
  iconLabel,
}) => {
  className = `button${primary ? " button__primary" : ""}${
    disabled || loading ? " button__disabled" : ""
  } ${loading ? " button__loading" : ""} ${danger ? " button__danger" : ""} ${
    minimal ? " button__minimal" : ""
  } ${icon ? " button__icon" : ""}${className || ""}`;

  if (to) {
    return (
      <Link
        to={to}
        className={className}
        {...(disabled && { onClick: (e) => e.preventDefault() })}
      >
        <span>{children}</span>
      </Link>
    );
  }

  return (
    <button
      onClick={onClick}
      className={className}
      disabled={disabled}
      type={type}
    >
      <span>{children}</span>
      {icon && iconLabel && (
        <span className="visually-hidden">{iconLabel}</span>
      )}
      {loading && (
        <div className="button__loading-container">
          <Loading />
        </div>
      )}
    </button>
  );
};

export default Button;
