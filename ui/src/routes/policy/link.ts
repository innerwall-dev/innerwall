// editorPath is the policy editor at one ruleset, by the name the
// listing holds; with no name, the editor at its first ruleset. Every
// "open in policy editor" in the console links here.
export function editorPath(ruleset?: string | null): string {
	return ruleset
		? `/policy?${new URLSearchParams({ ruleset }).toString()}`
		: "/policy";
}
