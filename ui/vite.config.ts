import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// The build output in dist/ is embedded into the control-plane binary via
// go:embed (see ../ui/embed.go and ADR-0008). Assets use relative paths so the
// control plane can serve the SPA from any mount point.
export default defineConfig({
	plugins: [react()],
	base: "./",
	build: {
		outDir: "dist",
		emptyOutDir: true,
	},
});
