import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

afterEach(() => {
	cleanup();
	localStorage.clear();
	document.documentElement.className = "";
	delete document.documentElement.dataset.theme;
	// biome-ignore lint/suspicious/noDocumentCookie: clears the sidebar's persisted state between tests
	document.cookie = "sidebar_state=; path=/; max-age=0";
});
