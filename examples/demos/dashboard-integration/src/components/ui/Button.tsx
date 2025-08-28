import React from "react";
import { cn } from "@/lib/utils";

interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: "default" | "outline" | "ghost";
  size?: "sm" | "default" | "lg";
}

const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant = "default", size = "default", ...props }, ref) => {
    return (
      <button
        className={cn(
          "inline-flex items-center justify-center rounded-md text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:opacity-50 disabled:pointer-events-none ring-offset-background cursor-pointer",
          {
            "bg-slate-900 text-white hover:bg-slate-800": variant === "default",
            "border border-slate-300 bg-transparent text-slate-900 hover:bg-slate-100":
              variant === "outline",
            "hover:bg-slate-100": variant === "ghost",
          },
          {
            "h-8 px-3 text-xs": size === "sm",
            "h-10 py-2 px-4": size === "default",
            "h-11 px-8": size === "lg",
          },
          className,
        )}
        ref={ref}
        {...props}
      />
    );
  },
);
Button.displayName = "Button";

export { Button };
