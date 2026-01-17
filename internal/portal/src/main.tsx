import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./app";
import CONFIGS from "./config";
import hexToHSL from "./utils/hexToHsl";

// Set theme preference
const searchParams = new URLSearchParams(window.location.search);
const queryTheme = CONFIGS.FORCE_THEME || searchParams.get("theme");

if (queryTheme === "dark" || queryTheme === "light") {
  // Save new theme preference
  localStorage.setItem("theme", queryTheme);
  document.body.setAttribute("data-theme", queryTheme);
} else {
  // Use saved theme preference, default to light if none exists
  const savedTheme = localStorage.getItem("theme") ?? "light";
  document.body.setAttribute("data-theme", savedTheme);
}

// Apply metadata configs
if (CONFIGS.FAVICON_URL) {
  const favicon =
    document.querySelector('link[rel="icon"]') ||
    document.createElement("link");
  favicon.setAttribute("rel", "icon");
  favicon.setAttribute("href", CONFIGS.FAVICON_URL);
  document.head.appendChild(favicon);
}
if (CONFIGS.ORGANIZATION_NAME) {
  document.title = `${CONFIGS.ORGANIZATION_NAME} – Event Destinations Portal`;
}

// Create color variants derived from brand color and override css variables from global.scss
if (CONFIGS.BRAND_COLOR) {
  const hsl = hexToHSL(CONFIGS.BRAND_COLOR);

  const { h, s, l } = hsl;

  let colors = {
    primary: `hsl(${h}, ${s}%, ${l}%)`,
    primaryHover: `hsl(${h}, ${s}%, ${l - 8}%)`,
    containerPrimary: `hsl(${h}, ${s}%, 90%)`,
    containerPrimaryHover: `hsl(${h}, ${s}%, 70%)`,
    foregroundPrimary: `hsl(${h}, ${s}%, ${l}%)`,
    foregroundContainerPrimary: `hsl(${h}, ${s}%, ${l - 12}%)`,
    outlinePrimary: `hsl(${h}, ${s}%, 80%)`,
  };

  if (document.body.dataset.theme === "dark") {
    colors = {
      primary: `hsl(${h}, ${s}%, ${l}%)`,
      primaryHover: `hsl(${h}, ${s}%, ${l + 8}%)`,
      containerPrimary: `hsl(${h}, ${s}%, 20%)`,
      containerPrimaryHover: `hsl(${h}, ${s}%, 25%)`,
      foregroundPrimary: `hsl(${h}, ${s}%, ${l}%)`,
      foregroundContainerPrimary: `hsl(${h}, ${s}%, ${l + 12}%)`,
      outlinePrimary: `hsl(${h}, ${s}%, 30%)`,
    };
  }

  const mapping = {
    "--colors-background-primary": colors.primary,
    "--colors-background-primary-hover": colors.primaryHover,
    "--colors-background-container-primary": colors.containerPrimary,
    "--colors-background-container-primary-hover": colors.containerPrimaryHover,
    "--colors-foreground-primary": colors.foregroundPrimary,
    "--colors-foreground-container-primary": colors.foregroundContainerPrimary,
    "--colors-outline-primary": colors.outlinePrimary,
    "--colors-shadow-button-primary": "0px 1px 2px 0px rgba(0, 0, 0, 0.16)",
  };

  Object.entries(mapping).forEach(([key, value]) => {
    document.body.style.setProperty(key, value);
  });
}

const container = document.getElementById("root") as HTMLElement;

const root = createRoot(container);

root.render(
  <StrictMode>
    <App />
  </StrictMode>,
);
