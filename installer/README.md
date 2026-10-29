# KoboRoot packaging

For a release bundle, run `./build-release.ps1 -Version 0.1`. This builds and tests
the installers, then creates `dist/Receive-Share-v0.1.zip` with all three variants,
release instructions, the MIT license and installer checksums. A separate
`.zip.sha256` file verifies the download. Use `-SkipInstallers` only to bundle
installers that were already built from the desired source. Release notes live in
`docs/releases/v0.1.md`. Creating the bundle does not tag or publish a release.

Run `./build-installers.ps1` from the repository root on Windows. Use `-SkipBuild`
only when `package/` already contains the desired ARM receiver and licenses.

The standard output `dist/KoboRoot.tgz` contains only managed application files,
documentation/licenses and `.adds/nm/kobo-receive`. It deliberately has no settings,
port preference, books, runtime state, existing menu configurations or dependency
binaries. Kobo's ordinary archive extraction is sufficient; there is no installer
hook or replacement boot script.

Optional variants include NickelDBus only (`with-nickeldbus/`) or both NickelMenu
and NickelDBus (`first-install/`). These are **not conditional installers**: the
recipient must choose the variant for missing dependencies. Never use a bundle
to silently replace an unknown or newer dependency. NickelMenu's example doc is
stored under the app's docs directory rather than replacing `.adds/nm/doc`.

Pinned sources:

- [NickelMenu v0.6.0](https://github.com/pgaskin/NickelMenu/releases/tag/v0.6.0), MIT.
- [NickelDBus 0.2.0](https://github.com/shermp/NickelDBus/releases/tag/0.2.0), MIT.

Archive and license hashes are pinned in `main.go`. Dependencies are read only
after checksum verification; unexpected files or links fail packaging. Files in
the resulting archive have root ownership, POSIX modes, relative Unix paths and
normalized shell-script line endings. Configuration preservation is checked by
the tests. Artifacts are deterministic for identical inputs.

These packages have been inspected and tested locally. They have not yet been
installed through a real Kobo update/restart cycle. The embedded application
itself has already been tested on the Libra 2 via SSH deployment.
