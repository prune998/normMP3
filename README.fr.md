**Français** | [**English**](README.md)

![CI](https://github.com/prune998/normMP3/actions/workflows/ci.yml/badge.svg)

# NormMP3 — Normalisation du niveau sonore des fichiers MP3

NormMP3 ramène un lot de fichiers MP3 à un niveau sonore commun (le
**gain cible**, 89 dB par défaut). C'est le portage natif en Go de
l'application Tkinter originale écrite par Elie Couzinié
(`test_mp3gain_tk-Copie.py`).

Tout est intégré : décodage MP3, analyse de loudness ReplayGain,
traitement de gain/normalisation et ré-encodage MP3. **Ni `mp3gain.exe`,
ni `ffmpeg.exe`, aucun outil externe, aucune dépendance à
l'exécution** — un seul binaire autonome d'une dizaine de Mo, disponible
pour macOS (Intel, Apple Silicon et universel), Windows et Linux
(x86-64 et ARM64), compilé avec CGO désactivé.

## Fonctionnement

1. **Fichiers ▸ Choisir les fichiers** — sélectionnez un ou plusieurs
   MP3 dans le navigateur intégré. Chaque fichier est *copié* dans un
   dossier `Fichiers_normalises/` créé **dans le dossier qui contient le
   fichier original** : les originaux ne sont jamais modifiés.
2. **Action ▸ Analyse** — chaque fichier est décodé et mesuré avec
   l'algorithme ReplayGain 1.0 (RMS filtré égalisation
   psychoacoustique, 95e percentile). Le tableau affiche le niveau
   (Niveau), la cible (Cible) et la correction (Corr.) à appliquer.
3. **Action ▸ Traitement** — chaque fichier est corrigé :
   - correction inférieure à 0,5 dB → rien à faire (*Aucune
     correction*, vert) ;
   - correction jusqu'à 5 dB → amplification simple de
     `correction + 0,4 dB` (*Ampl. simple*, bleu) ; le réglage de
     +0,4 dB reprend celui de l'application d'origine ;
   - correction supérieure à 5 dB → normalisation du loudness
     (*Normalisation*, rouge).
   Les fichiers qui écrêteraient encore après l'amplification passent
   automatiquement une seconde fois par la normalisation (avec
   limiteur de crêtes).
4. **Action ▸ Choix du gain cible** — curseur de 85 à 93 dB par pas de
   0,5 dB. Le modifier relance l'analyse. La valeur est mémorisée dans
   un fichier `memo.txt`.

Un bouton **? Aide** dans la barre d'outils ouvre un guide intégré (en
français) qui explique tout cela : le fonctionnement, l'utilisation et
l'emplacement exact des MP3 normalisés.

Pendant l'analyse et le traitement, une barre de progression et une
ligne d'état indiquent l'avancement.

Un **lecteur intégré** se trouve sous la liste « Sélection » : cliquez
sur un fichier pour charger sa copie normalisée, puis lecture/pause et
déplacement **±10 secondes** avec les boutons prévus — aucun lecteur
externe nécessaire. (Le double clic vers le lecteur du système a été
remplacé par ce lecteur intégré.)

Les balises ID3v2 des fichiers (titre, artiste, album, pochette…) sont
reportées sur les fichiers ré-encodés.

## Téléchargement et installation

Récupérez l'archive correspondant à votre machine sur la
[page des versions](https://github.com/prune998/normMP3/releases) et
suivez le fichier `INSTALLATION.txt` qu'elle contient :

| Votre machine | Archive |
|---|---|
| Mac Apple Silicon (M1…) | `NormMP3-<version>-macos-apple-silicon.zip` |
| Mac Intel | `NormMP3-<version>-macos-intel.zip` |
| Mac, si vous hésitez | `NormMP3-<version>-macos-universal.zip` (fonctionne sur les deux) |
| PC Windows habituel | `NormMP3-<version>-windows-amd64.zip` |
| PC Windows ARM (Snapdragon, Surface ARM) | `NormMP3-<version>-windows-arm64.zip` |
| Linux PC 64 bits | `NormMP3-<version>-linux-amd64.tar.gz` |
| Linux ARM 64 bits (Raspberry Pi…) | `NormMP3-<version>-linux-arm64.tar.gz` |

Le fichier `SHA256SUMS.txt` joint à chaque version liste les empreintes
SHA-256 de toutes les archives.

## macOS : ouvrir l'application au premier lancement (déquarantaine)

NormMP3 est distribué en dehors du Mac App Store et son binaire est
signé *ad hoc* (pas de compte Apple Developer payant, pas de
notarisation). Quand on télécharge une application de ce type, macOS
lui attache un attribut de **quarantaine** (`com.apple.quarantine`) et
Gatekeeper refuse alors de l'ouvrir. Selon la version de macOS, l'un de
ces messages apparaît :

- *« NormMP3 ne peut pas être ouvert car le développeur ne peut pas
  être vérifié »*
- *« NormMP3 ne peut pas être ouvert car il provient d'un développeur
  non identifié »*
- *« NormMP3 est endommagé et ne peut pas être ouvert. Vous devriez le
  placer dans la Corbeille »* — ce mot « endommagé » est celui que
  macOS utilise pour les applications non signées porteuses de
  l'attribut de quarantaine ; **l'application n'est pas réellement
  endommagée**.

L'une quelconque des méthodes suivantes lève le blocage. Elle n'est
nécessaire qu'**une seule fois**, juste après le téléchargement ;
ensuite l'application s'ouvre normalement.

### Méthode 1 — Clic droit ▸ Ouvrir (la plus simple)

1. Décompressez l'archive et glissez `NormMP3.app` dans `Applications`
   (ou laissez-la où vous voulez).
2. **Clic droit** (ou Ctrl-clic) sur `NormMP3.app` et choisissez
   **Ouvrir**.
3. Dans la boîte de dialogue, cliquez encore sur **Ouvrir**.
   Ensuite, le double-clic fonctionne normalement.

### Méthode 2 — Réglages Système

1. Essayez d'ouvrir l'application (un dialogue apparaît et la bloque —
   c'est attendu).
2. Ouvrez **Réglages Système ▸ Confidentialité et sécurité**, puis
   descendez jusqu'à la section **Sécurité**.
3. Vous y trouverez *« NormMP3 a été bloqué car son développeur n'est
   pas identifié »* — cliquez sur **Ouvrir quand même**, puis
   authentifiez-vous (mot de passe ou Touch ID).

### Méthode 3 — Terminal (fonctionne toujours)

Ouvrez le Terminal et exécutez :

```sh
xattr -dr com.apple.quarantine /Applications/NormMP3.app
```

(adaptez le chemin si vous avez gardé l'application ailleurs, par
exemple `~/Téléchargements/NormMP3.app`). Cette commande supprime
l'attribut de quarantaine du paquet ; Gatekeeper cesse immédiatement
de se plaindre.

Pour vérifier avant/après :

```sh
xattr -l /Applications/NormMP3.app      # liste les attributs ; cherchez com.apple.quarantine
```

### Pourquoi ?

Le travail de Gatekeeper est de protéger les Mac contre les logiciels
non signés téléchargés sur internet. Une vraie chaîne « Developer ID +
notarisation » suppose un compte Apple Developer payant et l'envoi de
chaque version à Apple pour analyse. NormMP3 est un petit outil libre
distribué via GitHub : il n'embarque qu'une signature ad hoc. La
déquarantaine ci-dessus est la méthode standard et documentée pour
ouvrir ce type d'application ; c'est le cas de toutes les applications
Mac non signées, rien de spécifique à NormMP3.

## Remarques d'utilisation

- Les copies normalisées sont placées dans un dossier
  `Fichiers_normalises/` créé à côté de chaque fichier original :
  importez depuis plusieurs dossiers, chacun garde son lot normalisé.
- L'application conserve ses fichiers de préférences (`memo.txt` gain
  cible, `normmp3.conf` thème) dans le dossier depuis lequel elle
  démarre. Lancée depuis le Finder (macOS) ou par double-clic
  (Windows), elle utilise votre dossier personnel en secours. Pour
  travailler sur un dossier musical précis, démarrez l'application
  depuis ce dossier (voir le `INSTALLATION.txt` livré avec chaque
  archive).
- Le ré-encodage se fait à 64 kbps, comme l'application d'origine. La
  fréquence d'échantillonnage des sources est conservée et **les
  fichiers mono restent mono** (pas de conversion en stéréo).
- Les originaux ne sont jamais modifiés : tout se passe dans
  `Fichiers_normalises/`.

## Compilation à partir des sources

Nécessite Go 1.27 ou plus (pas de compilateur C, pas de cgo) et un accès
réseau au premier lancement pour récupérer les archives ffmpeg :

```sh
git clone https://github.com/prune998/normMP3.git
cd normMP3
make check          # gofmt + go vet + tests
make run            # lance l'application sur cette machine
make build          # binaire natif dans build/
make dist           # tous les paquets possibles pour cette machine dans dist/
make dist-macos dist-windows dist-linux   # les sept paquets
```

Cibles : `make dist-macos` (intel / apple-silicon / universel, nécessite
un Mac pour `lipo`, `codesign` et `ditto`), `make dist-windows`,
`make dist-linux` (compilation croisée pure depuis n'importe quelle
machine).

## Notes techniques

| Aspect | Mise en œuvre |
|---|---|
| Interface | [shirei](https://pkg.go.dev/go.hasen.dev/shirei) v0.8.0 — immediate mode, multiplateforme, Go pur (Metal/D3D11/GLES via purego, rendu logiciel en repli) |
| Analyse de loudness | ReplayGain 1.0, porté depuis `gain_analysis.c` (Robinson/Sawyer/Klemm) — le même algorithme que mp3gain, validé contre la référence C compilée |
| Traitement et ré-encodage | **ffmpeg embarqué dans le binaire** (builds statiques téléchargés par `scripts/fetch-ffmpeg.sh`, compressés en xz, extraits dans le cache utilisateur au premier lancement), avec les commandes exactes de l'application d'origine : `volume=<corr+0,4>dB` / `loudnorm=I=-(112-cible):TP=-2:LRA=7 -ar 44100 -ab 64k` |
| Balises | les métadonnées ID3 sont reportées par la gestion par défaut de ffmpeg |

Le traitement utilise le vrai ffmpeg : les passes volume et loudnorm se
comportent exactement comme dans l'application d'origine (mêmes filtres,
même sortie 44,1 kHz / 64 kbps).

Taille du binaire : le ffmpeg embarqué alourdit sensiblement le
téléchargement (≈100 Mo par archive). Windows ARM64 n'a pas de build
statique disponible : sur cette plateforme l'application utilise le
`ffmpeg` trouvé dans le PATH, et le traitement affiche un message clair
si aucun n'est installé.

La compilation nécessite les archives ffmpeg (go:embed) :

```sh
scripts/fetch-ffmpeg.sh all    # ou : make ffmpeg-bins (automatique via make build/dist)
```

## Crédits

- Application et méthode de travail d'origine : **Elie Couzinié**
- Analyse ReplayGain : David Robinson, Glen Sawyer, Frank Klemm
  (LGPL-2.1+ — portée dans `internal/rgain`)
- Framework d'interface : shirei de hasenj (Zlib)

Licence : voir [LICENSE](LICENSE).
