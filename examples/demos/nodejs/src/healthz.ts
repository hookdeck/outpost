import outpost from "./lib/outpost";

const main = async () => {
  let ok = false;
  try {
    await outpost.health.check();
    ok = true;
  } catch (err: unknown) {
    const status =
      err && typeof err === "object" && "statusCode" in err
        ? (err as { statusCode: number }).statusCode
        : undefined;
    if (status === 404) {
      console.log("Health endpoint not available (e.g. managed Outpost). Skipping.");
      ok = true;
    } else {
      console.error(err);
    }
  }
  console.log(`Health check: ${ok ? "OK" : "FAIL"}`);
};

main()
  .then(() => {
    console.log("Done");
    process.exit(0);
  })
  .catch((err) => {
    console.error("Error", err);
    process.exit(1);
  });
