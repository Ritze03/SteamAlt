package assets

import "testing"

func TestEmbedded(t *testing.T) {
	for i, b := range [][]byte{GeistRegular, GeistMedium, GeistBold, GeistMonoRegular, NerdFont, IconPNG, IconSVG} {
		if len(b) == 0 {
			t.Errorf("asset %d is empty", i)
		}
	}
}
