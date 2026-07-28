const webUrl = process.env.MMDASH_SMOKE_URL ?? "http://localhost:3000";
const projectsUrl = new URL("/projects", webUrl);
const response = await fetch(projectsUrl);

if (!response.ok) {
  throw new Error(`Web smoke check failed with HTTP ${response.status}`);
}

const html = await response.text();
if (!html.includes("选择数学建模或研究项目进入工作区")) {
  throw new Error("Web smoke check did not find the project-list shell.");
}

const apiResponse = await fetch(`${webUrl}/api/example`);
if (!apiResponse.ok) {
  throw new Error(`API smoke check failed with HTTP ${apiResponse.status}`);
}

const body = await apiResponse.json();
if (body.status !== "ok" || body.storage !== "postgres") {
  throw new Error(`Unexpected API response: ${JSON.stringify(body)}`);
}

console.log("Web -> BFF -> Core -> PostgreSQL smoke check passed.");
