/// <reference types="vitest/config" />
import { fileURLToPath } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// The build output in dist/ is embedded into the control-plane binary
// (see embed.go and ADR-0008) and served by the operator listener at the
// root of its origin, so assets use absolute paths and a deep link into
// the console resolves them the same way the entry point does. Fonts are
// bundled from their packages; the console makes no external request.
export default defineConfig({
	plugins: [react(), tailwindcss()],
	base: "/",
	resolve: {
		alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
	},
	build: {
		outDir: "dist",
		emptyOutDir: true,
	},
	server: {
		// The development server proxies the API to a running control
		// plane, which serves TLS with a certificate the proxy need not
		// trust; the browser then talks to one origin, as it does in
		// production, so the cookie and the origin guard behave the same.
		proxy: {
			"/api": {
				target: process.env.INNERWALL_OPERATOR_URL ?? "https://127.0.0.1:8080",
				secure: false,
				changeOrigin: false,
			},
		},
	},
	test: {
		environment: "happy-dom",
		setupFiles: ["./src/test/setup.ts"],
		css: false,
		include: ["src/**/*.test.{ts,tsx}"],
	},
});
