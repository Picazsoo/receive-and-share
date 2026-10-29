# On-device checks

These scripts run on the Kobo through an already secured SSH connection.

- `test-finish.sh` tests a running receiver's local Finish & refresh library endpoint,
  including legacy browser origin headers, and waits for `pfmDoneProcessing`.
- `deploy-receiver.sh` verifies `kobo-receive.new` against
  `/tmp/kobo-receive-new.sha256`, stops the receiver, backs up its executable,
  installs the new one, and starts it. Transfer both staged files first.
- `clean-menu.sh` installs staged launcher/menu files and retires the setup and
  diagnostic entries outside NickelMenu's configuration folder.
- `screenshot.go` reads the current framebuffer geometry and pixels, and writes
  a PNG to standard output without changing the display. Build it for ARM and
  place it at `/tmp/kobo-screenshot` on the Kobo. `capture-screen.mjs` downloads
  the PNG over the existing pinned, key-only SSH connection; pass the private
  client-state directory as its first argument and an output path as its second.

SSH installation and credentials are intentionally separate from the receiver.

`security-device.mjs` uses the same pinned SSH state to check that a rejected Host
with an incomplete body closes promptly, and that a newly created disposable hidden
PDF cannot be listed, downloaded, reviewed or zipped through the real FAT `ADDS~1`
alias. It requires Share all to be enabled already, leaves sharing settings alone,
and removes only its unique test file. It never reads existing hidden book contents.

`smoke-device.mjs` runs from the computer with Node.js and the existing pinned SSH
client state (`node tools/smoke-device.mjs ../.local-ssh`). It pairs over real Wi-Fi,
uploads two uniquely named disposable PDFs, checks both download routes and ZIP
payloads, and removes only its own test files. It tests legacy native-browser Origin
headers too. It renews the source Kobo's pairing code and requires an awake device.

`fault-proxy.mjs` is a loopback-only development server for reconnect testing. It
forwards :4182 to a local receiver on :4181, with POST controls on :4183 for
`offline`, `online`, `cut-upload-once`, and `drop-ack-once`. It is never installed
on the Kobo.
