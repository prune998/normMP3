## Normalisation MP3 @VERSION@

Application de normalisation du niveau sonore des fichiers MP3
(portage Go de l'application Tkinter originale d'Elie Couzinié).
Aucune dépendance externe : analyse, traitement et ré-encodage sont
intégrés au binaire.

## Installation

Téléchargez l'archive correspondant à votre machine, puis suivez le
fichier `INSTALLATION.txt` qu'elle contient.

| Votre machine | Archive |
|---|---|
| Mac Apple Silicon (M1 …) | `NormMP3-@VERSION@-macos-apple-silicon.zip` |
| Mac Intel | `NormMP3-@VERSION@-macos-intel.zip` |
| Mac, si vous hésitez | `NormMP3-@VERSION@-macos-universal.zip` (fonctionne sur les deux) |
| PC Windows habituel | `NormMP3-@VERSION@-windows-amd64.zip` |
| PC Windows ARM (Snapdragon, Surface ARM) | `NormMP3-@VERSION@-windows-arm64.zip` |
| Linux PC 64 bits | `NormMP3-@VERSION@-linux-amd64.tar.gz` |
| Linux ARM 64 bits (Raspberry Pi…) | `NormMP3-@VERSION@-linux-arm64.tar.gz` |

En résumé :

- **macOS** — décompressez, glissez `NormMP3.app` dans Applications,
  puis **clic droit ▸ Ouvrir** au premier lancement.
- **Windows** — décompressez, puis double-cliquez sur `normmp3.exe`.
- **Linux** — décompressez, puis lancez `./NormMP3/normmp3` depuis un
  terminal ouvert dans votre dossier de musique.

## Empreintes

Le fichier `SHA256SUMS.txt` liste les empreintes SHA-256 de toutes les
archives (`sha256sum -c SHA256SUMS.txt`).
