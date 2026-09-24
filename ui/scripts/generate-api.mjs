// Generates src/api/generated.ts, the console's types for the operator
// surface, from its hand-authored description at api/openapi.yaml. The
// generated file is committed and never edited; the drift check
// regenerates it and fails when the committed file differs, so a change
// to the description without regeneration cannot land.
import { writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import openapiTS, { astToString } from "openapi-typescript";

const spec = new URL("../../api/openapi.yaml", import.meta.url);
const out = fileURLToPath(new URL("../src/api/generated.ts", import.meta.url));

const ast = await openapiTS(spec, { alphabetize: true });
const header =
	"// Generated from api/openapi.yaml by scripts/generate-api.mjs. Do not edit.\n\n";
writeFileSync(out, header + astToString(ast));
console.log(`wrote ${out}`);
