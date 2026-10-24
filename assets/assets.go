// Package assets embeds the fonts and app icon.
package assets

import _ "embed"

var (
	//go:embed fonts/Geist-Regular.ttf
	GeistRegular []byte
	//go:embed fonts/Geist-Medium.ttf
	GeistMedium []byte
	//go:embed fonts/Geist-Bold.ttf
	GeistBold []byte
	//go:embed fonts/GeistMono-Regular.ttf
	GeistMonoRegular []byte
	//go:embed fonts/GeistMonoNerdFont-Regular.otf
	NerdFont []byte
	//go:embed icons/icon-steamalt-128.png
	IconPNG []byte
	//go:embed icons/icon-steamalt.svg
	IconSVG []byte
)
