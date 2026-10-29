# Screenshot gallery

[Back to the project README](../../README.md)

Click any image for its full size. All 27 screenshots are shown below.
The Kobo images are real Libra 2 framebuffer captures. Client images use disposable
sample books. Mobile views use a 390 × 844 desktop browser viewport, not a physical
phone. The finished page uses the production template with dummy receipts.

## Kobo Libra 2

### Connection screen

[<img src="kobo-connection.png" alt="Connection screen" width="350">](kobo-connection.png)

### Connection screen with test receipt

[<img src="kobo-connection-received.png" alt="Connection screen with test receipt" width="350">](kobo-connection-received.png)

### Books to share

[<img src="kobo-sharing.png" alt="Books to share" width="350">](kobo-sharing.png)

### Device name

[<img src="kobo-device-name.png" alt="Device name" width="350">](kobo-device-name.png)

### Received this session

[<img src="kobo-session-files.png" alt="Received this session" width="350">](kobo-session-files.png)

## Desktop and mobile client

### Pairing

| Desktop | Phone viewport |
| --- | --- |
| [<img src="desktop-pair.jpg" alt="Desktop: Pairing" width="640">](desktop-pair.jpg) | [<img src="mobile-pair.jpg" alt="Phone viewport: Pairing" width="240">](mobile-pair.jpg) |

### Upload queue

| Desktop | Phone viewport |
| --- | --- |
| [<img src="desktop-upload-queue.jpg" alt="Desktop: Upload queue" width="640">](desktop-upload-queue.jpg) | [<img src="mobile-upload-queue.jpg" alt="Phone viewport: Upload queue" width="240">](mobile-upload-queue.jpg) |

### Upload in progress

| Desktop | Phone viewport |
| --- | --- |
| [<img src="desktop-upload-progress.jpg" alt="Desktop: Upload in progress" width="640">](desktop-upload-progress.jpg) | [<img src="mobile-upload-progress.jpg" alt="Phone viewport: Upload in progress" width="240">](mobile-upload-progress.jpg) |

### Uploads received

| Desktop | Phone viewport |
| --- | --- |
| [<img src="desktop-upload-complete.jpg" alt="Desktop: Uploads received" width="640">](desktop-upload-complete.jpg) | [<img src="mobile-upload-complete.jpg" alt="Phone viewport: Uploads received" width="240">](mobile-upload-complete.jpg) |

### File browser

| Desktop | Phone viewport |
| --- | --- |
| [<img src="desktop-files.jpg" alt="Desktop: File browser" width="640">](desktop-files.jpg) | [<img src="mobile-files.jpg" alt="Phone viewport: File browser" width="240">](mobile-files.jpg) |

### Bulk-download selection

| Desktop | Phone viewport |
| --- | --- |
| [<img src="desktop-selected.jpg" alt="Desktop: Bulk-download selection" width="640">](desktop-selected.jpg) | [<img src="mobile-selected.jpg" alt="Phone viewport: Bulk-download selection" width="240">](mobile-selected.jpg) |

### File details

| Desktop | Phone viewport |
| --- | --- |
| [<img src="desktop-file-details.jpg" alt="Desktop: File details" width="640">](desktop-file-details.jpg) | [<img src="mobile-file-details.jpg" alt="Phone viewport: File details" width="240">](mobile-file-details.jpg) |

### Basic / Kobo client

| Desktop | Phone viewport |
| --- | --- |
| [<img src="desktop-basic.jpg" alt="Desktop: Basic / Kobo client" width="640">](desktop-basic.jpg) | [<img src="mobile-basic.jpg" alt="Phone viewport: Basic / Kobo client" width="240">](mobile-basic.jpg) |

### Basic bulk-download review

| Desktop | Phone viewport |
| --- | --- |
| [<img src="desktop-basic-review.jpg" alt="Desktop: Basic bulk-download review" width="640">](desktop-basic-review.jpg) | [<img src="mobile-basic-review.jpg" alt="Phone viewport: Basic bulk-download review" width="240">](mobile-basic-review.jpg) |

### Connection lost

| Desktop | Phone viewport |
| --- | --- |
| [<img src="desktop-connection-lost.jpg" alt="Desktop: Connection lost" width="640">](desktop-connection-lost.jpg) | [<img src="mobile-connection-lost.jpg" alt="Phone viewport: Connection lost" width="240">](mobile-connection-lost.jpg) |

### Finished session (sample render)

| Desktop | Phone viewport |
| --- | --- |
| [<img src="desktop-finished-sample.jpg" alt="Desktop: Finished session (sample render)" width="640">](desktop-finished-sample.jpg) | [<img src="mobile-finished-sample.jpg" alt="Phone viewport: Finished session (sample render)" width="240">](mobile-finished-sample.jpg) |

## Local gallery and capture tools

For the HTML gallery, run `node tools/screenshot-gallery.mjs` from the repository
root and visit http://127.0.0.1:4190/.

- `tools/screenshot-session.mjs` starts the production host receiver on port 4188 with isolated storage in dist.
- `tools/screenshot-proxy.mjs` provides a throttled loopback proxy on port 4189.
- `SCREENSHOT_FIXTURE_DIR` enables `go test -run TestExportScreenshotFixture` to export the finished-page template.
- `node tools/build-screenshot-gallery.mjs` rebuilds this Markdown page and the HTML gallery after captures.

Screenshots are taken through browser controls. None of these fixture servers or
sample books are part of the Kobo installation.

[Back to the project README](../../README.md)
