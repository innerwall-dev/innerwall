import { useEffect, useState } from "react";

// useDebounced is a value that follows another once it has held still
// for the given time: what a live preview reads, so typing does not
// send a request per keystroke.
export function useDebounced<T>(value: T, ms: number): T {
	const [held, setHeld] = useState(value);
	useEffect(() => {
		const t = setTimeout(() => setHeld(value), ms);
		return () => clearTimeout(t);
	}, [value, ms]);
	return held;
}
