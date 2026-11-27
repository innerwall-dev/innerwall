# Third-party notices

Innerwall's own source code is licensed under Apache-2.0 (see [LICENSE](LICENSE)). The control-plane binary also embeds the built console, which contains code and font files from third-party packages, each under its own license:

- **Fonts.** The console carries three font families as variable font files vendored in `ui/src/fonts/`, each beside its own license: Inter 4.001, © 2016 The Inter Project Authors (`ui/src/fonts/OFL-Inter.txt`), and Manrope 4.504, © 2019 The Manrope Project Authors (`ui/src/fonts/OFL-Manrope.txt`), both copied from the `@fontsource-variable/inter` and `@fontsource-variable/manrope` packages (5.3.0) with each package's license; and Source Code Pro 1.026, © 2023 Adobe, with Reserved Font Name 'Source' (`ui/src/fonts/OFL-SourceCodePro.txt`, Adobe's upstream license). All three are licensed under the [SIL Open Font License 1.1](https://openfontlicense.org/open-font-license-official-text/), not Apache-2.0.
- **Code.** The console's production dependency tree (`ui/package.json` and what it pulls in) is under permissive licenses: MIT, ISC, BSD-3-Clause, 0BSD, and Apache-2.0.

The full license text of every one of those packages and font families is written into the built console on every build, read from the installed packages and the vendored license file so it cannot fall behind the lockfile or the fonts (`ui/scripts/notices.ts`):

- `THIRD-PARTY-NOTICES.txt` at the console's root: every package and font family, its version and license, and the license text as it ships.
- `assets/<font>-LICENSE.txt` beside the font files: each font family's license in full.

Both are embedded in the binary with the rest of the console. CI fails a console build that lacks them.
