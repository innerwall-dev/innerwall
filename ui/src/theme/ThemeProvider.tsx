import {
	createContext,
	type ReactNode,
	useCallback,
	useContext,
	useEffect,
	useMemo,
	useState,
} from "react";

export type Theme = "dark" | "light";

// storageKey is where the preference lives; index.html's pre-paint
// script reads the same key, and resolves the theme the same way
// (resolveTheme), so the stored theme never flashes.
export const storageKey = "innerwall.console.theme";

interface ThemeContextValue {
	theme: Theme;
	setTheme: (theme: Theme) => void;
}

const ThemeContext = createContext<ThemeContextValue | null>(null);

function readStored(): Theme | null {
	try {
		const v = localStorage.getItem(storageKey);
		return v === "dark" || v === "light" ? v : null;
	} catch {
		return null;
	}
}

function systemTheme(): Theme {
	return typeof matchMedia === "function" &&
		matchMedia("(prefers-color-scheme: dark)").matches
		? "dark"
		: "light";
}

// resolveTheme is the theme a load opens in: a stored preference wins,
// and the system preference is the default, including when the store
// refuses to be read.
export function resolveTheme(): Theme {
	return readStored() ?? systemTheme();
}

// ThemeProvider owns the root element's data-theme attribute, which the
// design tokens key on, the .dark class that mirrors it, and the page's
// color-scheme meta, which the browser paints the canvas in before the
// stylesheet arrives. Every change is written back so the next load and
// the pre-paint script agree.
export function ThemeProvider({ children }: { children: ReactNode }) {
	const [theme, setThemeState] = useState<Theme>(resolveTheme);

	useEffect(() => {
		const root = document.documentElement;
		root.dataset.theme = theme;
		root.classList.toggle("dark", theme === "dark");
		const scheme = document.querySelector<HTMLMetaElement>(
			'meta[name="color-scheme"]',
		);
		if (scheme) scheme.content = theme;
	}, [theme]);

	const setTheme = useCallback((next: Theme) => {
		setThemeState(next);
		try {
			localStorage.setItem(storageKey, next);
		} catch {
			// a browser that refuses storage still gets the theme for this load
		}
	}, []);

	const value = useMemo(() => ({ theme, setTheme }), [theme, setTheme]);
	return (
		<ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
	);
}

export function useTheme(): ThemeContextValue {
	const ctx = useContext(ThemeContext);
	if (!ctx) {
		throw new Error("useTheme called outside ThemeProvider");
	}
	return ctx;
}
