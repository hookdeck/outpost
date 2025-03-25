import * as DropdownMenu from "@radix-ui/react-dropdown-menu";
import { FC, PropsWithChildren } from "react";
import Button from "../Button/Button";
import "./Dropdown.scss";
import Badge from "../Badge/Badge";

interface DropdownProps {
  trigger_icon?: React.ReactNode;
  trigger: React.ReactNode;
  disabled?: boolean;
  badge_count?: number;
  badge_variant?: "primary" | "success" | "danger";
}

const Dropdown: FC<PropsWithChildren<DropdownProps>> = ({
  trigger_icon,
  trigger,
  children,
  badge_count,
  badge_variant,
  disabled = false,
}) => {
  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger asChild disabled={disabled}>
        <div>
          <Button>
            {trigger_icon}
            {trigger}
            {badge_count && badge_count > 0 ? (
              <Badge
                text={String(badge_count)}
                primary={badge_variant === "primary"}
                success={badge_variant === "success"}
                danger={badge_variant === "danger"}
              />
            ) : null}
          </Button>
        </div>
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content
          className="dropdown-content"
          sideOffset={4}
          align="start"
        >
          {children}
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  );
};

export default Dropdown;
