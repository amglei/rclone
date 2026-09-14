# FUTURE — Netzwerk-Backends für alle Zeitstempel (Metadata-Support)

Ziel des Fork-Features: Android-Dateizeiten (atime/mtime/ctime, btime via
Fallback) vollständig nach Windows transportieren. Lokal→lokal funktioniert
bereits komplett (siehe `ANDROID_WINDOWS_TIMES.md`). Was fehlt: Metadata-
Support in rclones Netzwerk-Backends — damit der Fluss auch über LAN
(Termux-sshd bzw. Windows-Freigabe) ohne Cloud-Relais funktioniert.

## Ausgangslage (verifiziert am 14.09.2026, rclone v1.75.1-termux-local)

- **sftp, smb, webdav haben keinerlei Metadata-Support** — im Source geprüft:
  kein `Metadata()`/`ReadMetadata`, `backend/sftp/sftp.go:2591` liest nur
  `Mtime` aus dem stat.
- rclones `serve`-Protokolle (sftp/webdav/http/nfs) exponieren ihrerseits nur
  mtime → Server/Client-Rollen-Vertauschung löst nichts.
- Praxistest `<Testordner>` (Gerät → Windows via Termux-sshd):
  mtime korrekt (Dateien + Unterordner), atime wurde durch
  `backend/local/local.go:996` (`os.Chtimes(path, t, t)`) auf mtime gesetzt,
  CreationTime = Kopierzeit; Root-Ordner wird von rclone nie gesetzt
  (`fs/operations/operations.go:2750` erreicht die Transferwurzel nicht).
- Metadata-fähig sind heute nur `local`, `memory`, `crypt` (Passthrough) und
  einzelne Clouds (Drive `createdTime`, OneDrive `createdDateTime` → `btime`).

## Entscheidung: vorhandene Backends erweitern, kein neues Backend

Begründung: Der Datenkanal (Transfer, Listings, Retry, Tests) ist in
sftp/smb fertig; es fehlt nur die Metadata-Mappung in das bestehende
rclone-Framework (`fs.Metadata`, `Features().ReadMetadata/SetMetadata`,
`--metadata-mapper`, Doku-Tabellen). Ein neues Backend würde das alles neu
erfinden und ist upstream kaum mergbar (AGENTS.md: keine Core-Hacks,
kleinste Änderung, bestehende Muster nutzen).

## Stufe 1 — sftp-Backend: `ReadMetadata` (klein, deckt den Anwendungsfall)

**STATUS: umgesetzt und abgenommen (14.09.2026).** Implementierung in
`backend/sftp/sftp.go`: `Object.Metadata` (re-stat beim Lesen) +
`Directory.Metadata` (neuer Directory-Typ aus dem Listing) +
`metadataFromFileStat` (`mode`/`uid`/`gid`/`atime`/`mtime`, Formate wie im
local-Backend), Features `ReadMetadata` + **`ReadDirMetadata`** gesetzt,
`systemMetadataInfo` + `MetadataInfo` deklariert, Metadata-Sektion in
`docs/content/sftp.md`, Unit-Tests in `backend/sftp/metadata_internal_test.go`.

Wichtig dabei: Ohne das Feature-Flag `ReadDirMetadata` am QUELL-Backend
schaltet sync auf den `MkdirModTime`-Pfad um (`fs/sync/sync.go:162`) und
überträgt nur `mtime` — `CopyDirMetadata` mit volles-Metadaten-Transfer
läuft erst mit beiden Flags (`ReadDirMetadata` + `WriteDirMetadata`).

Abnahme (`rclone copy :sftp,… /storage/…/<Testordner> %TEMP%\… -M
--metadata-btime-from-oldest --create-empty-src-dirs`, Android 10):
Dateien **und** Unterordner → LastWriteTime = Quelle.mtime,
LastAccessTime = Quelle.atime, CreationTime = min(atime, mtime). Beispiele:
trace.log Erstellt 23.03. 15:02:33 / Geändert 01.04. 01:02:41; test.log
Erstellt 01:02:10 / Geändert 01:02:41. Bekannt und OK: Wurzel-Ordner wird
von rclone nie gesetzt (upstream `fs/operations`), NTFS-ctime nicht
schreibbar, Zeitpräzision 1 s (SFTP v3).

Prinzip: SFTP-`STAT` (v3) liefert **atime + mtime** (plus mode/uid/gid);
`pkg/sftp.FileStat` hat beide Felder. Funktioniert gegen **unverändertes
OpenSSH-sshd** (Termux!) — keine Server-Seite nötig.

- `backend/sftp/sftp.go`:
  - `func (o *Object) Metadata(ctx) (fs.Metadata, error)` aus dem stat:
    `atime`, `mtime` (RFC 3339), `mode`, `uid`, `gid`.
  - `f.Features().ReadMetadata = true` setzen; optional `UnWrap`-Konform wie
    andere Backends.
  - Präzision: Protokoll liefert uint32 Sekunden — dokumentieren.
- **Effekt am Windows-Ziel** (local, mit Fork): Geändert + Zugriff korrekt,
  und `--metadata-btime-from-oldest` setzt **Erstellt = min(atime, mtime)**.
  Beispiel test.log: atime 01:02:10.87 < mtime 01:02:41.32 → Erstellt
  = 01:02:10 (heute: Kopierzeit).
- **Bewusste Grenzen**: `ctime`/`btime` sind im SFTP-Protokoll nicht
  vorgesehen. Akzeptabel: NTFS-ctime ist ohnehin nicht schreibbar, und
  `ctime ≥ mtime` heißt, ctime gewinnt im `min()`-Fallback nie.
- Optional danach:
  - `SetMetadata` (SFTP `SETSTAT`: atime/mtime) → Push-Richtung
    Windows → Android mit Zeiten.
  - `serve sftp`: Metadata-Exposition für rclone↔rclone.
  - Doku: Metadata-Tabelle in `docs/content/sftp.md`, `metadata.md`-Liste
    der unterstützten Backends erweitern (`make backenddocs` nicht committen).
- Tests: Unit-Test mit lokalem `rclone serve sftp`-Roundtrip bzw.
  `fstests`-Muster; Metadata-Map-Inhalte gegen Goldwerte prüfen.

## Stufe 2 — smb-Backend: alle vier Zeitstempel (protokoll-nativ)

SMB2 trägt Creation/Access/Write/Change nativ mit hoher Präzision;
`go-smb2.FileStat` hat `CreateTime/AccessTime/LastWriteTime/ChangeTime`.

- `ReadMetadata`: alle vier → `btime`, `atime`, `mtime`, `ctime`.
- `SetMetadata`: benötigt `SET_FILE_INFORMATION` (FileBasicInformation) —
  `go-smb2` unterstützt das Setzen von CreationTime evtl. nicht → ggf.
  Patch/PR in `go-smb2` oder Raw-Implementierung.
- Server-Seite: realistisch eine **native Windows-Freigabe** (Richtung:
  Android pusht auf Windows). Termux als SMB-Server ist unpraktisch.
- Ergebnis: 100 % Fidelity inkl. echtem btime/ctime (nur hier möglich).
- Mehr Aufwand als Stufe 1; separat folgen lassen.

## Fallback ohne Codeänderung (Workaround-Skript)

sftp-Pull (mtime stimmt) + `ssh stat`-Abgleich der Quelle + PowerShell-
Zeitsetzung am Ziel (CreationTime/LastAccessTime/LastWriteTime). Funktioniert
sofort, ist aber ein Pfusch-Skript statt rclone-nativ — nur als Überbrückung.

## Abnahme (nach Stufe 1)

Wiederholung des Testlaufs vom 14.09.2026, Quelle
`/storage/emulated/0/<Testordner>` (Android 10, Termux-sshd):

1. `rclone copy :sftp,host=<host>,port=<port>:/storage/…/<Testordner> %TEMP%\<Ziel> -M --metadata-btime-from-oldest --create-empty-src-dirs`
2. Gegenprüfung Dateien/Unterordner: LastWriteTime = Quelle.mtime,
   LastAccessTime = Quelle.atime, CreationTime = min(atime, mtime).
3. Bekannt und OK: Root-Ordner-Mtime (rclone setzt Transferwurzel nie — ggf.
   separater Upstream-Fix in `fs/operations`), NTFS-ctime.
