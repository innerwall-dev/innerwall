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

// storageKey is where the preference lives; index.html reads the same
// key before first paint so the stored theme never flashes.
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

// ThemeProvider owns the .dark class on the root element: a stored
// preference wins, the system preference is the default, and every
// change is written back so the next load and the pre-paint script agree.
export function ThemeProvider({ children }: { children: ReactNode }) {
	const [theme, setThemeState] = useState<Theme>(
		() => readStored() ?? systemTheme(),
	);

	useEffect(() => {
		document.documentElement.classList.toggle("dark", theme === "dark");
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
