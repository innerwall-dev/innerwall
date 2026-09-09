// Application shell. Surfaces land in product order (ADR-0001): flow map,
// policy editor, simulation, inventory, enrollment. Each lives in its own
// directory under src/ and talks only to the public REST façade (ADR-0007).
export function App() {
	return (
		<main>
			<h1>Innerwall</h1>
			<p>Control-plane UI shell. Surfaces arrive with milestone M3.</p>
		</main>
	);
}
