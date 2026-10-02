[**Français**](README.fr.md) | **English**

![CI](https://github.com/prune998/normMP3/actions/workflows/ci.yml/badge.svg)

# NormMP3 — MP3 loudness normalization

NormMP3 brings a set of MP3 files to a common loudness level (the
**target gain**, 89 dB by default). It is a native Go port of the
original Tkinter application written by Elie Couzinié
(`test_mp3gain_tk-Copie.py`).

Everything is built in: MP3 decoding, ReplayGain loudness analysis,
gain/normalization processing and MP3 re-encoding. **No `mp3gain.exe`,
no `ffmpeg.exe`, no external tool, no runtime dependency** — one
self-contained binary of about 10 MB, available for macOS (Intel,
Apple Silicon and universal), Windows and Linux (x86-64 and ARM64),
compiled with CGO disabled.

## How it works

1. **Files ▸ Choose files** — pick one or more MP3 files in the built-in
   browser. Each one is *copied* into a `Fichiers_normalises/` folder
   created **inside the folder containing the original file**, so the
   originals are never touched.
2. **Action ▸ Analysis** — each file is decoded and measured with the
   ReplayGain 1.0 algorithm (equal-loudness filtered RMS, 95th
   percentile). The table shows the level (Niveau), the target (Cible)
   and the correction (Corr.) to apply.
3. **Action ▸ Processing** — each file is corrected:
   - correction below 0.5 dB → nothing to do (*Aucune correction*, green);
   - correction up to 5 dB → simple volume boost of `correction + 0.4 dB`
     (*Ampl. simple*, blue); the +0.4 dB trim matches the original app;
   - correction above 5 dB → loudness normalization (*Normalisation*, red).
   Files that would still clip after the boost are automatically passed
   through the normalization stage again (with a peak limiter).
4. **Action ▸ Target gain** — slider from 85 to 93 dB in 0.5 dB steps.
   Changing it re-runs the analysis. The value is remembered in a
   `memo.txt` file.

A **? Help** button in the toolbar opens an in-app guide (in French)
covering all of this: how the app works, how to use it, and exactly
where the normalized MP3s are written.

During analysis and processing a progress bar and a status line show
where things stand.

A **built-in player** sits below the selection list: click a file in the
list to load its normalized copy, then play/pause it and jump **±10
seconds** with the seek buttons — no external player needed. (Double-click
to open your system player is gone; the built-in player replaces it.)

The ID3v2 tags of the files (title, artist, album, artwork…) are
carried over to the re-encoded files.

## Download and install

Grab the archive matching your machine from the
[releases page](https://github.com/prune998/normMP3/releases) and follow
the `INSTALLATION.txt` inside it:

| Your machine | Archive |
|---|---|
| Mac Apple Silicon (M1…) | `NormMP3-<version>-macos-apple-silicon.zip` |
| Mac Intel | `NormMP3-<version>-macos-intel.zip` |
| Mac, if unsure | `NormMP3-<version>-macos-universal.zip` (runs on both) |
| Usual Windows PC | `NormMP3-<version>-windows-amd64.zip` |
| Windows ARM PC (Snapdragon, Surface ARM) | `NormMP3-<version>-windows-arm64.zip` |
| Linux 64-bit PC | `NormMP3-<version>-linux-amd64.tar.gz` |
| Linux ARM 64-bit (Raspberry Pi…) | `NormMP3-<version>-linux-arm64.tar.gz` |

Each `SHA256SUMS.txt` attached to a release lists the SHA-256
fingerprints of all archives.

## macOS: opening the app the first time (unquarantining)

NormMP3 is distributed outside the Mac App Store and its binary is
signed *ad hoc* (no paid Apple Developer ID, no notarization). When you
download an app like this, macOS attaches a **quarantine attribute**
(`com.apple.quarantine`) to it, and Gatekeeper then refuses to open it.
Depending on the macOS version you will see one of these messages:

- *“NormMP3 cannot be opened because the developer cannot be verified”*
- *“NormMP3 cannot be opened because it is from an unidentified developer”*
- *“NormMP3 is damaged and can't be opened. You should move it to the
  Trash”* — the “damaged” wording is the one macOS uses for unsigned
  apps that carry the quarantine attribute; **the app is not actually
  damaged**.

Any one of the following removes the block. It is only needed **once**,
right after downloading; afterwards the app opens normally.

### Option 1 — Right-click open (easiest)

1. Decompress the zip and drag `NormMP3.app` to `Applications` (or leave
   it wherever you like).
2. **Right-click** (or Ctrl-click) on `NormMP3.app` and choose **Open**.
3. In the dialog that appears, click **Open** again.
   From then on, double-clicking works as usual.

### Option 2 — System Settings

1. Try to open the app once (a dialog appears and blocks it — that's
   expected).
2. Open **System Settings ▸ Privacy & Security** and scroll to the
   **Security** section.
3. You will find *“NormMP3 was blocked from use because it is not from
   an identified developer”* — click **Open Anyway**, then authenticate
   with your password or Touch ID.

### Option 3 — Terminal (always works)

Open Terminal and run:

```sh
xattr -dr com.apple.quarantine /Applications/NormMP3.app
```

(Adjust the path if you kept the app elsewhere, e.g. `~/Downloads/NormMP3.app`.)
This deletes the quarantine attribute from the bundle; Gatekeeper stops
complaining immediately.

If you prefer to check before/after:

```sh
xattr -l /Applications/NormMP3.app      # lists attributes; look for com.apple.quarantine
```

### Why is it like this?

Gatekeeper's job is to protect Macs from unsigned software downloaded
from the internet. A proper Apple "Developer ID + notarization" chain
requires a paid Apple Developer account and sending each release to
Apple for scanning. NormMP3 is a small free tool distributed via
GitHub, so it ships with an ad-hoc signature only. The quarantine dance
above is the standard, documented way of opening such apps; it is the
same for every unsigned Mac application, not something specific to
NormMP3.

## Usage notes

- Normalized copies land in a `Fichiers_normalises/` folder created next
  to each original file — import from several folders and each keeps its
  own normalized set.
- The app keeps its preference files (`memo.txt` target gain,
  `normmp3.conf` theme) in the folder it is started from. Launched from
  the Finder (macOS) or by double-click (Windows), it falls back to your
  home folder. To work on a specific music folder, start the app from
  that folder (see the `INSTALLATION.txt` shipped with each archive).
- Re-encoding happens at 64 kbps, like the original application. The
  sample rate of the source is preserved and **mono files stay mono**
  (no stereo conversion).
- Originals are never modified: everything happens inside
  `Fichiers_normalises/`.

## Building from source

Requires Go 1.27+ (no C compiler needed, no cgo, no external library):

```sh
git clone https://github.com/prune998/normMP3.git
cd normMP3
make check          # gofmt + go vet + tests
make run            # run the app on this machine
make build          # native binary in build/
make dist           # all packages for this machine in dist/
make dist-macos dist-windows dist-linux   # all seven packages
```

Targets: `make dist-macos` (intel / apple-silicon / universal, needs a
Mac for `lipo`, `codesign` and `ditto`), `make dist-windows`,
`make dist-linux` (pure cross-compilation from any machine).

## Technical notes

| Concern | Implementation |
|---|---|
| GUI | [shirei](https://pkg.go.dev/go.hasen.dev/shirei) v0.8.0 — immediate-mode, cross-platform, pure Go (Metal/D3D11/GLES via purego, software fallback) |
| MP3 decoding | [hajimehoshi/go-mp3](https://pkg.go.dev/github.com/hajimehoshi/go-mp3) (pure Go) |
| Loudness analysis | ReplayGain 1.0, ported from `gain_analysis.c` (Robinson/Sawyer/Klemm), validated against the compiled C reference |
| Re-encoding | [braheezy/shine-mp3](https://pkg.go.dev/github.com/braheezy/shine-mp3) (Shine fixed-point encoder port), 64 kbps |
| Tags | [bogem/id3v2/v2](https://pkg.go.dev/github.com/bogem/id3v2/v2) — frames carried over on re-encode |

The "loudness normalization" stage approximates ffmpeg's
`loudnorm=I=-(112-target):TP=-2:LRA=7`: the measured gain is applied and
a soft-knee limiter holds peaks at -2 dBFS.

## Credits

- Original application and workflow: **Elie Couzinié**
- ReplayGain analysis: David Robinson, Glen Sawyer, Frank Klemm
  (LGPL-2.1+ — ported in `internal/rgain`)
- GUI framework: hasenj's shirei (Zlib)

License: see [LICENSE](LICENSE).
