const message = process.argv.slice(2).join(" ").trim();
const conventionalCommit =
  /^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\([a-z0-9._/-]+\))?!?: .{1,100}$/;

if (!message) {
  console.error('Usage: pnpm commit:check -- "type(scope): summary"');
  process.exit(2);
}

if (!conventionalCommit.test(message)) {
  console.error(`Invalid Conventional Commit subject: ${message}`);
  process.exit(1);
}
