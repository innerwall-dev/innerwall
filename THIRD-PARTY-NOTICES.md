# Third-party notices

Innerwall's own source code is licensed under Apache-2.0 (see [LICENSE](LICENSE)). The control-plane binary also embeds the built console, which contains code and font files from third-party packages, each under its own license:

- **Fonts.** The console bundles its two font families from the font packages `ui/package.json` names. They are licensed under the [SIL Open Font License 1.1](https://openfontlicense.org/open-font-license-official-text/), not Apache-2.0.
- **Code.** The console's production dependency tree (`ui/package.json` and what it pulls in) is under permissive licenses: MIT, ISC, BSD-3-Clause, 0BSD, and Apache-2.0.

The full license text of every one of those packages is written into the built console on every build, read from the installed packages so it cannot fall behind the lockfile (`ui/scripts/notices.ts`):

- `THIRD-PARTY-NOTICES.txt` at the console's root: every package, its version and license, and the license text as the package ships it.
- `assets/<font>-LICENSE.txt` beside the font files: each font family's license in full.

Both are embedded in the binary with the rest of the console. CI fails a console build that lacks them.
