import type { ZudokuConfig } from "zudoku";
import { HeadNavigation } from "./src/components/HeadNavigation";
import { htmlPlugin } from "./src/plugins/htmlPlugin";
import process from "node:process";

const ZUDOKU_PUBLIC_CUSTOM_HEAD_SCRIPT =
  process.env.ZUDOKU_PUBLIC_CUSTOM_HEAD_SCRIPT || "";

const config: ZudokuConfig = {
  basePath: "/docs",
  metadata: {
    favicon: "https://outpost.hookdeck.com/docs/icon.svg",
    title: "%s | Outpost",
    description:
      "Outpost is an open source, self-hostable implementation of Event Destinations, enabling event delivery to user-preferred destinations like Webhooks, Hookdeck, AWS SQS, RabbitMQ, Kafka, and more.",
    generator: "Zudoku",
    applicationName: "Outpost Documentation",
    keywords: [
      "outpost",
      "event destinations",
      "webhooks",
      "send webhooks",
      "event delivery",
      "webhook delivery",
    ],
    publisher: "Hookdeck Technologies Inc.",
  },
  // theme: {
  //   code: {
  //     additionalLanguages: ["yaml"],
  //   },
  // },
  redirects: [
    { from: "/", to: "/overview" },
    { from: "/references", to: "/references/api" },
  ],
  plugins: [htmlPlugin({ headScript: ZUDOKU_PUBLIC_CUSTOM_HEAD_SCRIPT })],
  UNSAFE_slotlets: {
    "head-navigation-start": HeadNavigation,
  },
  page: {
    pageTitle: "",
    logoUrl: "/",
    logo: {
      src: {
        // TODO: Update once basePath is used by Zudoku
        // light: "logo/outpost-logo-black.svg",
        // dark: "logo/outpost-logo-white.svg"
        light:
          "https://outpost-docs.vercel.app/docs/logo/outpost-logo-black.svg",
        dark: "https://outpost-docs.vercel.app/docs/logo/outpost-logo-white.svg",
      },
      width: "110px",
    },
  },
  // mdx: {
  //   components: {
  //     YamlConfig,
  //   },
  // },
  topNavigation: [
    { id: "docs", label: "Documentation", default: "overview" },
    { label: "API Reference", id: "docs/api" },
  ],
  sidebar: {
    docs: [
      {
        type: "doc",
        label: "Overview",
        id: "overview",
      },
      {
        type: "category",
        label: "Quickstarts",
        collapsed: false,
        collapsible: false,
        items: [
          {
            type: "doc",
            label: "Docker",
            id: "quickstarts/docker",
          },
          {
            type: "doc",
            label: "Kubernetes",
            id: "quickstarts/kubernetes",
          },
        ],
      },
      {
        type: "doc",
        label: "Concepts",
        id: "concepts",
      },
      {
        type: "doc",
        label: "Features",
        id: "features",
      },
      {
        type: "category",
        label: "Guides",
        collapsed: false,
        collapsible: false,
        link: "guides",
        items: [
          {
            type: "doc",
            label: "Migrate to Outpost",
            id: "guides/migrate-to-outpost",
          },
          {
            type: "doc",
            label: "Publish from RabbitMQ",
            id: "guides/publish-from-rabbitmq",
          },
          {
            type: "doc",
            label: "Publish from SQS",
            id: "guides/publish-from-sqs",
          },
          {
            type: "doc",
            label: "Deployment",
            id: "guides/deployment",
          },
          {
            type: "doc",
            label: "Dashboard Design",
            id: "guides/dashboard-design",
          },
        ],
      },
      {
        type: "category",
        label: "References",
        collapsed: false,
        collapsible: false,
        items: [
          {
            type: "doc",
            label: "Configuration",
            id: "references/configuration",
          },
          {
            type: "doc",
            label: "Roadmap",
            id: "references/roadmap",
          },
          {
            type: "link",
            label: "API",
            href: "/docs/api",
          },
        ],
      },
    ],
  },

  apis: {
    type: "file",
    input: "./apis/openapi.yaml",
    navigationId: "docs/api",
  },
  docs: {
    files: "/pages/**/*.{md,mdx}",
  },
};

export default config;
