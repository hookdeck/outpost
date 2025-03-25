import { FC } from "react";
import "./Badge.scss";

interface BadgeProps {
  success?: boolean;
  danger?: boolean;
  primary?: boolean;
  text: string | number;
  size?: "s" | "m";
}

const Badge: FC<BadgeProps> = ({
  text,
  size = "m",
  success = false,
  danger = false,
  primary = false,
}) => {
  const className = `badge${success ? " badge__success" : ""}${
    danger ? " badge__danger" : ""
  }${primary ? " badge__primary" : ""}${size === "s" ? " badge__s" : ""}`;

  return <span className={className}>{text}</span>;
};

export default Badge;
