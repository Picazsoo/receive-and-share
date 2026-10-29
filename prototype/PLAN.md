# Richer interface — UI-first plan

## Agreed scope

- Phone-first browser UI, also comfortable on desktop.
- Select or drop multiple EPUB/PDF files, with a queue and individual progress.
- Show success, interruption, cancellation and retry per file.
- Browse all book folders in Kobo user storage. Hide settings/system folders,
  including `.kobo`, `.adds`, and symlinks that escape the allowed storage root.
- Download individual books or select multiple books for one ZIP download.
- Use a straightforward tool interface: plain headings, compact file rows,
  useful filetype icons, no decorative book covers or marketing copy.
- Selection review begins with count and total size, followed by every selected
  file's full path, filename, size, and type. Keep selections across folders.
- Keep Finish & import on the Kobo QR screen, matching the working receiver.

## Prototype

Open `index.html` directly, or run `node prototype/server.mjs` from the repo root
and visit http://127.0.0.1:4173. No framework, dependencies, build, or external assets.

The prototype starts with three sample uploads. Click Send files to watch the
queue. The sample PDF interrupts once to exercise Retry. You can choose your own
files; they stay in memory in this browser and are never uploaded. Once their
simulated transfers finish, they appear in Received Books in the mock library.

Downloads of sample books contain clearly named text previews. Original files
you selected yourself can be downloaded intact. Bulk selection creates a valid
STORE-only ZIP locally to make the interaction testable. This is a prototype
implementation, not the planned production ZIP architecture.

Use Phone/Desktop at the top to check both layouts. Reset demo restores sample
state. Closing/reloading discards all local selections and file objects.

### Basic/e-reader view and pairing preview

`basic.html` uses plain HTML links, forms, tables and simple CSS with no scripts.
Run the local Node server for its file list, selection review, ZIP downloads and
pairing form to work. This prototype server only serves fixed sample files.
`pair.html` accepts demo code **482713** and shows an error for other codes.
`pair-source.html` previews the sharing Kobo's short address and six-digit code.
These are interaction prototypes, not a real authentication implementation.

For Kobo-to-Kobo use, the destination Kobo should *pull* books from the source:
open the source address, pair, browse, download. Do not depend on the Kobo's file
upload picker working. Test native EPUB/PDF download and import on hardware.

### Proposed pairing design

- Use a short root URL such as `http://192.168.0.29:8080`, without a secret path.
  Consider port 80 if free. Friendly `.local` names are optional and require
  testing; don't depend on mDNS support in the stock Kobo browser.
- Show a random six-digit pairing code on the source Kobo, valid for two minutes.
  Cap attempts per code and per client and rotate/expire the code when locked.
- After pairing, issue a long random session secret in an HttpOnly cookie. Keep
  the secret out of the address the user types. Expire it when sharing ends.
- Keep hidden CSRF tokens for state-changing forms; the stock Kobo browser's
  Origin header is unreliable, as observed while developing the Finish button.
- Optionally require confirmation on the source Kobo for a new pairing request.
- Pairing provides access control, not transport encryption. HTTP transfers are
  still visible to a sufficiently positioned observer on the Wi-Fi network.
  Review this model before connecting it to real files.

## Production approach, after UI review

1. Extract the agreed UI into assets embedded in the Go receiver, keeping a
   basic, server-rendered path with no JavaScript for stock Kobo browsers.
2. Use XMLHttpRequest upload progress events, one active upload at a time, to
   match the device's modest resources. Retain streaming temporary-file writes,
   duplicate naming, validation, size limits, and atomic completion behavior.
3. Add token-authorized listing, search, and single-file streaming downloads,
   using filesystem-root confinement and excluding private/system directories.
4. For bulk downloads, create a short-lived selection ticket, then stream a ZIP
   from the Kobo. Use archive/zip with STORE (no recompression) so memory use is
   bounded and EPUBs/PDFs aren't needlessly recompressed. Keep relative folders
   to avoid filename collisions; preflight permissions and existence first.
5. Add device/network failures, expired sessions, files changed during download,
   free-space checks, and configurable selection/total-size limits.
6. Verify actual uploads and downloads in Android Fennec and iOS Safari, plus
   Kobo-to-Kobo native download/import on the Libra 2. The source Kobo's sharing
   screen remains lightweight; enhanced and basic views share backend checks.

## ZIP decision

Browser-built ZIPs are possible, but common implementations buffer the result
in phone memory. A streamed server-built ZIP is the default proposal: one normal
download on Android/iOS and no complete archive buffered on either device.

References: [Go archive/zip](https://pkg.go.dev/archive/zip),
[JSZip memory limitations](https://stuk.github.io/jszip/documentation/limitations.html).

## Review next

Choose the visual direction and confirm queue behavior, selection across folders,
download confirmation, and the library's default listing. Backend integration
starts after that review. No changes to the working Kobo receiver are made here.
