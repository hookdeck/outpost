import fs from "node:fs/promises";
import path from "node:path";
import { URL } from "node:url";
import { SyntaxHighlight } from "zudoku/ui/SyntaxHighlight";
const getRootDir = async () => {
  const currentDir = path.resolve(
    path.dirname(new URL(import.meta.url).pathname)
  );
  const parentDir = path.resolve(currentDir, "../../../");
  return parentDir;
};

const rootDir = await getRootDir();
const exampleFilePath = path.join(rootDir, ".outpost.yaml.example");
const yamlText = await fs.readFile(exampleFilePath, "utf8");

export const YamlConfig = () => {
  return <SyntaxHighlight language="yaml" code={yamlText} />;
};
