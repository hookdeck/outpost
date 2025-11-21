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
      "Outpost is an open source, self-hostable implementation of Event Destinations, enabling event delivery to user-preferred destinations like Webhooks, Hookdeck, AWS SQS, AWS S3, RabbitMQ, Kafka, and more.",
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
  site: {
    title: "",
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
  navigation: [
    {
      type: "category",
      label: "Documentation",
      items: [
        {
          type: "doc",
          label: "Overview",
          file: "overview",
        },
        {
          type: "doc",
          label: "Concepts",
          file: "concepts",
        },
        {
          type: "category",
          label: "Quickstarts",
          link: "quickstarts",
          collapsed: false,
          collapsible: false,
          items: [
            {
              type: "doc",
              label: "Docker",
              file: "quickstarts/docker",
            },
            {
              type: "doc",
              label: "Kubernetes",
              file: "quickstarts/kubernetes",
            },
            {
              type: "doc",
              label: "Railway",
              file: "quickstarts/railway",
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
            "features/multi-tenant-support",
            "features/destinations",
            "features/topics",
            "features/publish-events",
            "features/event-delivery",
            "features/alerts",
            "features/tenant-user-portal",
            "features/opentelemetry",
            "features/logging",
            {
              type: "doc",
              label: "SDKs",
              file: "sdks",
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
              file: "guides/deployment",
            },
            {
              type: "doc",
              label: "Migrate to Outpost",
              file: "guides/migrate-to-outpost",
            },
            {
              type: "doc",
              label: "Publish from RabbitMQ",
              file: "guides/publish-from-rabbitmq",
            },
            {
              type: "doc",
              label: "Publish from SQS",
              file: "guides/publish-from-sqs",
            },
            {
              type: "doc",
              label: "Publish from GCP Pub/Sub",
              file: "guides/publish-from-gcp-pubsub",
            },
            {
              type: "doc",
              label: "Using Azure Service Bus as an Internal MQ",
              file: "guides/service-bus-internal-mq",
            },
            {
              type: "doc",
              label: "Building Your Own UI",
              file: "guides/building-your-own-ui",
            },
            {
              type: "doc",
              label: "Redis Troubleshooting",
              file: "guides/troubleshooting-redis",
            },
            {
              type: "doc",
              label: "Schema Migration",
              file: "guides/migration",
            },
          ],
        },
        {
          type: "category",
          label: "References",
          link: "references",
          collapsed: false,
          collapsible: false,
          items: [
            {
              type: "doc",
              label: "Configuration",
              file: "references/configuration",
            },
            {
              type: "doc",
              label: "Roadmap",
              file: "references/roadmap",
            },
            {
              type: "link",
              label: "API",
              to: "api/authentication",
            },
          ],
        },
      ],
    },
    {
      type: "link",
      label: "API Reference",
      to: "api/health",
    },
  ],
  apis: {
    type: "file",
    input: "./apis/openapi.yaml",
    path: "/api",
    options: {
      expandApiInformation: true,
      disablePlayground: true,
    },
  },
  docs: {
    files: "/pages/**/*.{md,mdx}",
  },
};

export default config;
