import * as process from "process";

import * as dotenv from "dotenv";
import { Outpost } from "@hookdeck/outpost-sdk";
dotenv.config();

if (!process.env.OUTPOST_API_BASE_URL || !process.env.OUTPOST_API_KEY) {
  console.error("OUTPOST_API_BASE_URL and OUTPOST_API_KEY are required");
  process.exit(1);
}

const outpost = new Outpost({
  serverURL: process.env.OUTPOST_API_BASE_URL,
  apiKey: process.env.OUTPOST_API_KEY,
});

export default outpost;
