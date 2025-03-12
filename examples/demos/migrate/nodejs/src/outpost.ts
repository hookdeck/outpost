import * as process from "process";

import * as dotenv from "dotenv";
dotenv.config();

if (!process.env.OUTPOST_API_BASE_URL || !process.env.OUTPOST_API_KEY) {
  console.error("OUTPOST_API_BASE_URL and OUTPOST_API_KEY are required");
  process.exit(1);
}

import OutpostClient from "./outpost-client";

const outpost = new OutpostClient(
  process.env.OUTPOST_API_BASE_URL,
  process.env.OUTPOST_API_KEY
);

export default outpost;
