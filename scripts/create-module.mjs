import {
  cp,
  mkdir,
  readFile,
  readdir,
  stat,
  writeFile,
} from "node:fs/promises";
import path from "node:path";

export const moduleNamePattern = /^[a-z][a-z0-9]*$/;

export async function createModule(name, root = process.cwd()) {
  if (!moduleNamePattern.test(name)) {
    throw new Error(
      "Module name must start with a lowercase letter and contain only lowercase letters and digits.",
    );
  }

  const templateRoot = path.join(root, "templates", "module");
  const destinationRoot = path.join(root, ".generated", name);
  await mkdir(path.dirname(destinationRoot), { recursive: true });
  await cp(templateRoot, destinationRoot, {
    errorOnExist: true,
    force: false,
    recursive: true,
  });
  await replaceTokens(destinationRoot, name);
  return destinationRoot;
}

async function replaceTokens(directory, name) {
  for (const entry of await readdir(directory)) {
    const entryPath = path.join(directory, entry);
    if ((await stat(entryPath)).isDirectory()) {
      await replaceTokens(entryPath, name);
      continue;
    }

    const contents = await readFile(entryPath, "utf8");
    await writeFile(entryPath, contents.replaceAll("__MODULE__", name), "utf8");
  }
}

const invokedDirectly =
  process.argv[1] &&
  path.resolve(process.argv[1]) === path.resolve(import.meta.filename);

if (invokedDirectly) {
  const name = process.argv[2] ?? "";
  try {
    const destination = await createModule(name);
    console.log(
      `Created module starter at ${path.relative(process.cwd(), destination)}`,
    );
    console.log(
      "Move each generated layer into its owning source directory after reviewing the README.",
    );
  } catch (error) {
    console.error(error instanceof Error ? error.message : error);
    process.exitCode = 1;
  }
}
