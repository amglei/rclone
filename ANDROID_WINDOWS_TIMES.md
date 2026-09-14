# Android-Zeiten lesen → Windows-Zeiten schreiben

Custom-Fork von rclone: Auf Android (Termux, non-root, ab Android 6) werden
`atime`/`mtime`/`ctime`/`btime` gelesen; auf Windows werden
Erstellt (`btime`), Geändert (`mtime`) und Zugriff (`atime`) geschrieben.
Fehlt `btime` in den Quell-Metadaten, wird die älteste verfügbare Zeit als
`btime` verwendet.

Nutzung:

```console
rclone copy -M --metadata-btime-from-oldest --create-empty-src-dirs <android-src> <windows-dst>
```

(`-M`/`--metadata` aktiviert die Metadata-Pipeline;
`--create-empty-src-dirs` ist rclones Bedingung dafür, dass
Verzeichnis-Metadaten überhaupt kopiert werden.)

## Änderungen im Überblick

### 1. `ctime` als System-Metadata-Key freigelegt

- **Wo:** `backend/local/metadata.go` (System-Key-Tabelle `systemMetadataInfo`),
  `backend/local/metadata_linux.go` (`readMetadataFromFileStatx`,
  `readMetadataFromFileFstatat`).
- **Was:** Neuer Key `"ctime"` (RFC 3339, „Time of last status change");
  statx-Pfad schreibt `setTime("ctime", stat.Ctime)`, fstatat-Pfad
  `setTime("ctime", stat.Ctim)`.
- **Warum:** rclone kannte `ctime` nur als Lesemodus für
  `--local-time-type ctime`, aber nicht als Metadata-Key. Ohne Key wäre
  `ctime` für den Metadata-Transfer (Quell-Backend → Ziel-Backend) unsichtbar.

### 2. statx auf Android per API-Level abgesichert (kein SIGSYS-Crash)

- **Wo:** `backend/local/metadata_linux.go` (Auswahl statx vs. fstatat),
  neu: `backend/local/statx_android.go`,
  `backend/local/statx_android_nocgo.go`,
  `backend/local/statx_other.go`.
- **Was:** Die bisherige Bedingung `runtime.GOOS != "android"` wurde durch
  `statxAllowed()` ersetzt. Auf Android (Termux = App-Prozess) ist statx nur
  ab **API 30 (Android 11)** erlaubt; darunter läuft immer der fstatat-Pfad.
  Die cgo-Variante liest das API-Level über `__system_property_get`,
  die nocgo-Variante best-effort aus `/system/build.prop` (unbekannt ⇒ sicher
  aus, also fstatat). Auf Nicht-Android-Linux unverändert `true`.
  Der `runtime`-Import in `metadata_linux.go` entfiel.
- **Warum:** Androids App-seccomp-Policy tötet Prozesse, die nicht
  allowlistete Syscalls aufrufen (`SECCOMP_RET_TRAP` → SIGSYS). `statx` steht
  erst seit Android 11 in `bionic/libc/SYSCALLS.TXT` (geprüft gegen AOSP für
  Android 10/11/12) und ist auf älteren Versionen ein Prozess-Kill ohne
  Fallback — siehe rclone-Issue #7006 (Crash auf Kernel 4.9, Android 10).
  Ein reiner Kernel-Check (`ENOSYS`) reicht nicht, weil seccomp den Prozess
  tötet, bevor ein Fehler zurückkehren kann. Zusätzlich braucht statx ohnehin
  Kernel ≥ 4.11 (`ENOSYS`-Fallback bleibt dafür erhalten).
- **Konsequenz:** `btime` ist auf Android nur ab API 30 lesbar (und auch dann
  nur, wenn der Kernel statx kann). Auf Android 6–10 liefert die Quelle
  `atime`/`mtime`/`ctime`, und der btime-Fallback (Punkt 3) springt ein.

### 3. Neue Flags `--metadata-btime-from-oldest` und `--metadata-btime-from`

- **Wo:** `fs/config.go` (Flag-Registrierung `metadata_btime_from_oldest` +
  Feld `ConfigInfo.MetadataBtimeFromOldest`), `fs/metadata.go`
  (`setBtimeFromOldest`, Hook in `GetMetadataOptions`),
  `docs/content/docs.md` (Flag-Abschnitt + `ctime` in der
  System-Metadata-Tabelle).
- **Was:** Wenn das Flag gesetzt ist und `btime` fehlt, wird `btime` auf das
  Minimum von `atime`/`mtime`/`ctime` (RFC3339Nano) gesetzt. Vorhandenes
  `btime` bleibt unangetastet; unparsbare Keys werden ignoriert.
- **Warum:** Der Einbauort `GetMetadataOptions` ist der zentrale Punkt, den
  alle schreibenden Backends (`Put`/`Update`/`Copy`, Verzeichnisse via
  `CopyDirMetadata`) aufrufen — die Logik greift damit automatisch für Dateien
  **und** Verzeichnisse. Ein `--metadata-mapper`-Skript hätte das auch
  gekonnt, wäre aber ein externer Prozess pro Datei und könnte `ctime` erst
  sehen, seit Punkt 1 es freilegt.
- **Hinweis:** Da jede Inhaltsänderung `ctime` mitsetzt, gilt immer
  `ctime ≥ mtime`, also `min(atime,mtime,ctime) == min(atime,mtime)` — `ctime`
  wird aus Vollständigkeit mitgeführt, ändert das Ergebnis aber praktisch nie.
- **Zweites Flag `--metadata-btime-from <atime|mtime|ctime>`** (Wo:
  `fs/config.go`, `fs/metadata.go`, `setBtimeFrom`): setzt `btime` aus genau
  dem gewählten Key statt aus dem Minimum aller Zeiten. Fehlt der gewählte
  Key, bleibt `btime` ungesetzt (Original-rclone-Verhalten). Beide Flags sind
  gegenseitig exklusiv — gemeinsame Nutzung bricht den Transfer mit einem
  Fehler ab, ungültige Werte ebenso. Tests: `TestMetadataBtimeFrom`.

### 4. Windows-Schreibseite: kein Code nötig

- `backend/local/setbtime_windows.go` (`haveSetBTime = true`,
  `syscall.SetFileTime(h, btime, atime, mtime)`) schreibt Erstellt/Geändert/
  Zugriff bereits vollständig, ausgelöst aus `writeMetadataToFile`
  (`backend/local/metadata.go`) — sobald die Keys in den Quell-Metadaten
  stehen (dafür sorgen Punkte 1–3).
- **NTFS-Change-Time bleibt nicht schreibbar** (Win32-`SetFileTime` kann sie
  nicht setzen; dafür wäre `NtSetInformationFile` nötig — bewusst außen vor).

### 5. Tests

- `fs/metadata_test.go`: `TestMetadataBtimeFromOldest` mit drei Fällen
  (älteste = atime / vorhandenes btime bleibt / nur mtime vorhanden).
- `backend/local/local_internal_test.go`: `TestMetadata` prüft jetzt auch
  `ctime` auf Linux (`hasCtime`).

## Besonderheiten / bekannte Grenzen

- **„Erstellt" aus dem Minimum ist eine Heuristik, kein echtes btime.**
  Mit `noatime`-Mounts bleibt `atime` stehen und kann näher an der Erstellung
  liegen als sonst — garantiert gleich der Erstellzeit ist es trotzdem nicht.
- `ctime` ist eine Inode-Änderungszeit (nicht portabel); reiner Lesekey, wird
  nie ans Ziel geschrieben.
- Dir-Metadaten brauchen `--create-empty-src-dirs` (rclone-Bedingung, kein
  Fork-Verhalten).
- Auf `GOOS=android` ist der Go-Build-Tag `linux` mitgesetzt — deshalb gilt
  `metadata_linux.go` (`//go:build linux`) auch für Android, und die drei
  neuen `statx_*.go`-Dateien trennen die Fälle per
  `android && cgo` / `android && !cgo` / `linux && !android`.

## Build-Howto

Voraussetzung: Go ≥ 1.26 (Repo: `go 1.26.0` in `go.mod`; getestet mit
go1.27.0). Unter Windows liegt `go.exe` ggf. nicht im PATH von cmd, sondern
unter `C:\Program Files\Go\bin\go.exe`.

### Android CLI (Termux), wie CI-Job `android` in `.github/workflows/build.yml`

NDK installieren (CI nutzt NDK 21, `RCLONE_NDK_VERSION=21`), dann pro ABI:

```console
GOOS=android GOARCH=arm64 CGO_ENABLED=1 \
CC=$ANDROID_NDK/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android21-clang \
CGO_LDFLAGS='-fuse-ld=lld -s -w' \
go build -v -tags android -trimpath -o rclone-android-arm64 .
```

Weitere ABIs: `GOARCH=arm` + `GOARM=7` (clang `armv7a-linux-androideabi21`),
`GOARCH=386` (`i686-...`), `GOARCH=amd64` (`x86_64-...`).

`CGO_ENABLED=1` ist nötig, damit `statx_android.go` (API-Level-Gate, sicherer
statx-Zugriff) greift; ohne cgo greift die nocgo-Variante (fstatat als
sicheres Default).

Alternativer Weg ohne lokales NDK-Setup: als installierbares Termux-Paket,
siehe [Termux-Paket mit Docker bauen](#termux-paket-mit-docker-bauen).

### Windows

```console
GOOS=windows GOARCH=amd64 go build -o rclone.exe .
```

### Verifikation

```console
go build ./...                                   # Gesamtbuild
go vet ./fs/ ./backend/local/                    # Vet
go test -count=1 ./fs/ ./backend/local/ ./fs/operations/   # Tests
```

Auf dem Gerät:

```console
rclone copy -M --metadata-btime-from-oldest --create-empty-src-dirs -vv <src> <dst>
rclone lsjson --metadata <src>   # zeigt atime, mtime, ctime, ggf. btime
```

Windows-Ziel prüfen mit
`Get-Item <file> | Select-Object CreationTime, LastWriteTime, LastAccessTime`.

## Termux-Paket mit Docker bauen

Empfohlener Weg für das Android-Binary: als Termux-Paket (`.deb`), da es sich
auf dem Gerät per `pkg install` installieren/aktualisieren lässt. Grundlage
ist das [termux-packages-Wiki](https://github.com/termux/termux-packages.wiki)
(gelesen: `Building-packages`, `Creating-new-package`, `Build-environment`).
Warum Docker: das Image `ghcr.io/termux/package-builder` bringt Ubuntu + NDK
genau so mit, wie die Termux-Maintainer bauen — reproduzierbar, kein lokales
NDK-Setup.

### Aufbau (beide Klone liegen im Repo-Root, bei Bedarf löschbar)

- `termux-packages/` — shallow-Klon des Build-Systems; Repo-Root wird in den
  Container nach `/home/builder/termux-packages` gemountet.
- `termux-packages.wiki/` — Wiki-Klon (Referenz).
- `termux-packages/sources/rclone/` — Kopie dieses Forks als **lokale
  Paketquelle** (39 MB; ausgeschlossen: `.git`, `.commandcode`,
  `termux-packages`, `termux-packages.wiki`).
- `termux-packages/packages/rclone/build.sh` — vom offiziellen Rezept
  (baut exakt v1.75.1) abgeleitet, drei Änderungen:

  ```shell
  TERMUX_PKG_SRCURL=file:///home/builder/termux-packages/sources/rclone
  TERMUX_PKG_SHA256=SKIP_CHECKSUM
  # -ldflags "-X github.com/rclone/rclone/fs.Version=v${TERMUX_PKG_VERSION}-termux-local.1"
  ```

  Hintergrund `file://…-dir`: der Container tarrt das Quellverzeichnis bei
  **jedem** Build neu (`.git` ausgenommen) — Quell-Änderungen brauchen weder
  Commit noch `-r`, ein Rebuild mit `-f` genügt.

### Build (Windows)

`.\scripts\run-docker.ps1` existiert zwar, scheitert aber ohne TTY-stdin
(`docker exec -it`) und hängt am Standard-Container; daher direkt:

```powershell
# einmalig, Image ~2-3 GB
docker pull ghcr.io/termux/package-builder

# Container (aus termux-packages/ heraus); --tty hält ihn am Leben,
# /dev/fuse ist zwingend (siehe Stolpersteine)
docker run --detach --tty --name termux-package-builder-rclone `
  --volume "${PWD}:/home/builder/termux-packages" `
  --security-opt "seccomp=${PWD}/scripts/profile.json" `
  --cap-add SYS_ADMIN --device /dev/fuse ghcr.io/termux/package-builder

# Build detached starten, Log landet auf dem gemounteten Volume
docker exec -d --user builder termux-package-builder-rclone bash -c `
  "cd /home/builder/termux-packages && mkdir -p output && ./build-package.sh -f -I rclone > output/build-rclone.log 2>&1"
```

`-f` erzwingt Neubau, `-I` zieht Dependencies aus dem Termux-Repo statt sie
zu bauen (rclone hat keine). Ergebnis:
`termux-packages/output/rclone_1.75.1_aarch64.deb`, Log:
`termux-packages/output/build-rclone.log`.

### Stolpersteine

- **`/dev/fuse` fehlt** → Abbruch im Toolchain-Setup
  (`fuse-overlayfs: cannot mount`), weil `termux_setup_toolchain_29.sh` ein
  fuse-Overlay über die NDK-Toolchain legt. Deshalb `--device /dev/fuse` +
  `--cap-add SYS_ADMIN` + seccomp-Profil — exakt wie in `run-docker.sh:95`.
- **Container stirbt sofort ohne `--tty`**: `run-docker.sh`/`run-docker.ps1`
  nutzen `--tty` beim `docker run`; ohne TTY beendet der Hauptprozess den
  Container, bevor `docker exec` läuft.
- **Build-Lock pro Container** („Another build is already running within
  same environment"): Läuft im Standard-Container `termux-package-builder`
  bereits ein Build (z. B. fremde Session), eigenen Container verwenden —
  Wiki-Abschnitt „Multiple Containers". Build-Cache ist pro Container
  (`$HOME/.termux-build` im Container-FS).
- **`docker exec -i` + Client-Abbruch** reißt den Build mit ab (SIGTERM);
  daher detached mit `-d` und Log auf dem Volume starten.

### Verifikation (ausgeführt)

```console
dpkg-deb -f output/rclone_1.75.1_aarch64.deb Package Version Architecture
dpkg-deb -x output/rclone_1.75.1_aarch64.deb /tmp/rcheck
file /tmp/rcheck/data/data/com.termux/files/usr/bin/rclone
go version -m <binary>          # GOOS/GOARCH/CGO_ENABLED der Binary
strings <binary> | grep -E 'termux-local|metadata_btime_from_oldest'
```

Ergebnis: ELF **ARM aarch64**, Interpreter `/system/bin/linker64`,
**Android 24** (Termux-Minimum), NDK r29; `fs.Version=v1.75.1-termux-local`,
`ctime`-Key und `metadata_btime_from_oldest` im Binary;
`GOOS=android GOARCH=arm64 CGO_ENABLED=1` → **cgo-Variante** des
statx-Gates aktiv, API-30-Schutz greift (unter Android 11 → fstatat ohne
btime → `--metadata-btime-from-oldest` übernimmt).

### Installation (Termux)

```console
pkg install ./rclone_1.75.1_aarch64.deb   # ersetzt/aktualisiert offizielles rclone
rclone version                            # → v1.75.1-termux-local.1
```

Installiert und geprüft auf einem Testgerät (Android 10, API 29, Kernel 4.14, aarch64, Termux) per `scp` + `apt install -y ./…deb`:
`rclone lsjson -M` liefert dort `atime`/`mtime`/`ctime`, **kein `btime`** —
korrekt, da das statx-Gate unter API 30 den fstatat-Pfad erzwingt; für
„Erstellt" auf Windows greift dann `--metadata-btime-from-oldest`. Das Paket
wurde am selben Tag mit dem sftp-ReadMetadata-Support neu gebaut und
installiert (FUTURE.md Stufe 1) — Pull Richtung Gerät → Windows über sshd
überträgt jetzt atime/mtime (Dateien + Ordner) und setzt „Erstellt" auf
min(atime, mtime).

### Weitere Architekturen / Rebuild

```console
docker start termux-package-builder-rclone
docker exec -d --user builder termux-package-builder-rclone bash -c \
  "cd /home/builder/termux-packages && ./build-package.sh -f -I -a arm rclone > output/build-arm.log 2>&1"
```

`-a`: `aarch64` (Default), `arm`, `i686`, `x86_64`. Geänderte Quelle einfach
neu nach `termux-packages/sources/rclone` kopieren und mit `-f` bauen — der
Container-Cache (Toolchain, Go, Modul-Cache) überlebt `docker stop`.

### Release-Artefakte

Alle vier Debs baut man sequenziell in **einer** `docker exec`-Zeile
(verkettet mit `&&` — parallel kollidiert mit dem Build-Lock). Release-Zip
für Windows mit Fork-Version und gestripptem Binary (wie offizielle Builds):

```console
go build -trimpath -ldflags "-s -w -X github.com/rclone/rclone/fs.Version=v1.75.1-termux-local.1" -tags noselfupdate -o rclone.exe .
powershell -Command "Compress-Archive -Path rclone.exe,README.md,AGENTS.md,COPYING -DestinationPath release\rclone-v1.75.1-termux-local.1-windows-amd64.zip"
```

Verifiziert am 15.09.2026 im `release/`-Ordner: Zip (rclone.exe 84 MB
gestrippt + README + AGENTS + COPYING) und vier Debs (aarch64/arm/i686/
x86_64), jedes Binary per String-Check mit dem sftp-ReadMetadata-Support
(FUTURE.md Stufe 1) belegt.

**Release 2** (Tag `v1.75.1-termux-local.1`): neues Flag
`--metadata-btime-from <atime|mtime|ctime>` (FUTURE.md „Erweiterung"), alle
vier Debs neu gebaut, Quell-Sync über robocopy nach
`termux-packages/sources/rclone` (`/MIR /XD .git .commandcode termux-packages
termux-packages.wiki release /XF rclone.exe`).

Hinweis: `TestNothingToTransferWithoutEmptyDirs` in `fs/sync` fällt auf
diesem Windows-Rechner auch **ohne** diese Änderungen durch (per
`git stash` verifiziert) — vorbestehendes Timing-Problem, kein Fork-Fehler.
