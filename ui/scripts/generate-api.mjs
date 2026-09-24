// Generates TypeScript types for the operator surface from its OpenAPI
// description when the description is present in the repository. The
// hand-written types in src/api/types.ts are the contract until then;
// once api/openapi.yaml lands, the generated file replaces them and this
// step becomes part of the drift check like every other generated file.
import { existsSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const spec = new URL("../../api/openapi.yaml", import.meta.url);
const out = fileURLToPath(new URL("../src/api/generated.ts", import.meta.url));

if (!existsSync(spec)) {
	console.log(
		"api/openapi.yaml is not present; the hand-written API types stand",
	);
	process.exit(0);
}

const { default: openapiTS, astToString } = await import("openapi-typescript");
const ast = await openapiTS(spec, { alphabetize: true });
const header =
	"// Generated from api/openapi.yaml by scripts/generate-api.mjs. Do not edit.\n\n";
writeFileSync(out, header + astToString(ast));
console.log(`wrote ${out}`);
