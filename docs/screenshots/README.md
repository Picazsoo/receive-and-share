# Screenshot gallery

Open [the gallery](index.html) or run `node tools/screenshot-gallery.mjs` from the repository root and visit http://127.0.0.1:4190/.

5 real Kobo framebuffer captures and 22 client screenshots. Client sample data is disposable and local. Mobile means a 390 × 844 browser viewport, not a physical phone test. The finished page is rendered from the production Go template with dummy receipts.

Fixture tools: `tools/screenshot-session.mjs` starts the production host receiver on 4188 with isolated storage in dist; `tools/screenshot-proxy.mjs` provides a throttled loopback proxy on 4189. Screenshots are taken through browser controls. `SCREENSHOT_FIXTURE_DIR` enables `go test -run TestExportScreenshotFixture` to export the finished-page template. `node tools/build-screenshot-gallery.mjs` rebuilds the gallery after captures. None of these fixture servers or sample books are part of the Kobo installation.

## Kobo Libra 2

- [Connection screen](kobo-connection.png)
- [Connection screen with test receipt](kobo-connection-received.png)
- [Books to share](kobo-sharing.png)
- [Device name](kobo-device-name.png)
- [Received this session](kobo-session-files.png)

## Client screens

| Screen | Desktop | Phone viewport |
| --- | --- | --- |
| Pairing | [View](desktop-pair.jpg) | [View](mobile-pair.jpg) |
| Upload queue | [View](desktop-upload-queue.jpg) | [View](mobile-upload-queue.jpg) |
| Upload in progress | [View](desktop-upload-progress.jpg) | [View](mobile-upload-progress.jpg) |
| Uploads received | [View](desktop-upload-complete.jpg) | [View](mobile-upload-complete.jpg) |
| File browser | [View](desktop-files.jpg) | [View](mobile-files.jpg) |
| Bulk-download selection | [View](desktop-selected.jpg) | [View](mobile-selected.jpg) |
| File details | [View](desktop-file-details.jpg) | [View](mobile-file-details.jpg) |
| Basic / Kobo client | [View](desktop-basic.jpg) | [View](mobile-basic.jpg) |
| Basic bulk-download review | [View](desktop-basic-review.jpg) | [View](mobile-basic-review.jpg) |
| Connection lost | [View](desktop-connection-lost.jpg) | [View](mobile-connection-lost.jpg) |
| Finished session — sample render | [View](desktop-finished-sample.jpg) | [View](mobile-finished-sample.jpg) |

[Back to the project README](../../README.md)
