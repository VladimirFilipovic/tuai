package clipboard

import "testing"

func TestIsImagePath(t *testing.T) {
	yes := []string{"a.png", "a.JPG", "shot.jpeg", "foo.gif", "bar.WEBP", "x.bmp"}
	no := []string{"a.txt", "a", "a.go", "a.html", "a.png.txt", ""}
	for _, p := range yes {
		if !IsImagePath(p) {
			t.Errorf("IsImagePath(%q) = false, want true", p)
		}
	}
	for _, p := range no {
		if IsImagePath(p) {
			t.Errorf("IsImagePath(%q) = true, want false", p)
		}
	}
}
