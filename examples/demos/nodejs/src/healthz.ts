import outpost from "./lib/outpost";

const main = async () => {
  const response = await outpost.healthz();
  console.log(`Health check: ${response ? "OK" : "FAIL"}`);
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
