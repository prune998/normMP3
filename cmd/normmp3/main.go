package main

import (
	app "go.hasen.dev/shirei/app"
	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

func main() {
	app.SetupWindow("Normalisation MP3", 1200, 640)
	app.Run(RootView)
}

func RootView() {
	Container(Attrs(Viewport, Background(200, 30, 95, 1)), func() {
		Container(Attrs(Row, CrossMid, Pad(4), Gap(2)), func() {
			MenuButton(NoIcon, "Fichiers", func() {})
			MenuButton(NoIcon, "Action", func() {})
			MenuButton(NoIcon, "A propos ...", func() {})
		})
		Container(Attrs(Expand, Grow(1), CrossMid), func() {
			Label("Scaffold: port of source/test_mp3gain_tk-Copie.py in progress")
		})
	})
}
