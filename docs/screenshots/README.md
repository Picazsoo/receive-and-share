# Screenshot gallery

Open [the gallery](index.html) or run `node tools/screenshot-gallery.mjs` from the repository root and visit http://127.0.0.1:4190/.

5 real Kobo framebuffer captures and 22 client screenshots. Client sample data is disposable and local. Mobile means a 390 × 844 browser viewport, not a physical phone test. The finished page is rendered from the production Go template with dummy receipts.

Fixture tools: `tools/screenshot-session.mjs` starts the production host receiver on 4188 with isolated storage in dist; `tools/screenshot-proxy.mjs` provides a throttled loopback proxy on 4189. Screenshots are taken through browser controls. `SCREENSHOT_FIXTURE_DIR` enables `go test -run TestExportScreenshotFixture` to export the finished-page template. `node tools/build-screenshot-gallery.mjs` rebuilds the gallery after captures. None of these fixture servers or sample books are part of the Kobo installation.
