import type { ZudokuConfig } from "zudoku";
import process from "node:process";

import { ApiAuthSideNav } from "./src/components/ApiAuthSideNav";
import { HeadNavigation } from "./src/components/HeadNavigation";
import { htmlPlugin } from "./src/plugins/htmlPlugin";

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
    { from: "/api", to: "/api/authentication" },
  ],
  plugins: [htmlPlugin({ headScript: ZUDOKU_PUBLIC_CUSTOM_HEAD_SCRIPT })],
  UNSAFE_slotlets: {
    "head-navigation-start": HeadNavigation,
    "zudoku-before-navigation": ApiAuthSideNav,
  },
  page: {
    pageTitle: "",
    logoUrl: "/",
    showPoweredBy: false,
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
    { label: "API Reference", id: "api/authentication" },
  ],
  sidebar: {
    docs: [
      {
        type: "doc",
        label: "Overview",
        id: "overview",
      },
      {
        type: "doc",
        label: "Concepts",
        id: "concepts",
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
          {
            type: "doc",
            label: "Railway",
            id: "quickstarts/railway",
          },
        ],
      },
      {
        type: "category",
        label: "Features",
        link: "features",
        collapsed: false,
        collapsible: false,
        items: [
          { type: "doc", id: "features/multi-tenant-support" },
          { type: "doc", id: "features/destinations" },
          { type: "doc", id: "features/topics" },
          { type: "doc", id: "features/publish-events" },
          { type: "doc", id: "features/event-delivery" },
          { type: "doc", id: "features/alerts" },
          { type: "doc", id: "features/tenant-user-portal" },
          { type: "doc", id: "features/opentelemetry" },
          { type: "doc", id: "features/logging" },
          {
            type: "doc",
            label: "SDKs",
            id: "sdks",
          },
        ],
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
            label: "Deployment",
            id: "guides/deployment",
          },
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
            label: "Publish from GCP Pub/Sub",
            id: "guides/publish-from-gcp-pubsub",
          },
          {
            type: "doc",
            label: "Using Azure Service Bus as an Internal MQ",
            id: "guides/service-bus-internal-mq",
          },
          {
            type: "doc",
            label: "Building Your Own UI",
            id: "guides/building-your-own-ui",
          },
          {
            type: "doc",
            label: "Redis Troubleshooting",
            id: "guides/troubleshooting-redis",
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
            href: "api/authentication",
          },
        ],
      },
    ],
  },
  apis: {
    type: "file",
    input: "./apis/openapi.yaml",
    navigationId: "/api",
    options: {
      disablePlayground: true,
    },
  },
  docs: {
    files: "/pages/**/*.{md,mdx}",
  },
};

export default config;
