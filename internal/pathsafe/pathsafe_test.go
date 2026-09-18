package pathsafe

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveWithin(t *testing.T) {
	base := t.TempDir()
	// EvalSymlinks, weil t.TempDir() auf macOS unter /var -> /private/var liegt.
	base, err := filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "b")
	sibling := filepath.Join(base, "bc")
	outside := filepath.Join(base, "draussen")
	for _, dir := range []string{root, sibling, outside, filepath.Join(root, "repo")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(root, "link-nach-draussen")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	roots := []string{root}
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{"Treffer im Root", filepath.Join(root, "repo"), filepath.Join(root, "repo"), nil},
		{"Root selbst", root, root, nil},
		{"Traversal über ..", filepath.Join(root, "repo", "..", "..", "draussen"), "", ErrOutsideRoots},
		{"Symlink aus dem Root heraus", link, "", ErrOutsideRoots},
		{"Präfix-Falle /a/bc gegen /a/b", sibling, "", ErrOutsideRoots},
		{"nicht existent, Elternpfad erlaubt", filepath.Join(root, "neu", "tiefer"), filepath.Join(root, "neu", "tiefer"), nil},
		{"nicht existent, außerhalb", filepath.Join(outside, "neu"), "", ErrOutsideRoots},
		{"leerer Pfad", "", "", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveWithin(roots, tc.input)
			switch {
			case tc.input == "":
				if err == nil {
					t.Fatal("erwartet Fehler bei leerem Pfad")
				}
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("Fehler = %v, erwartet %v", err, tc.wantErr)
				}
			default:
				if err != nil {
					t.Fatalf("unerwarteter Fehler: %v", err)
				}
				if got != tc.want {
					t.Fatalf("Pfad = %q, erwartet %q", got, tc.want)
				}
			}
		})
	}
}

func TestResolveWithinMehrereRoots(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := filepath.Join(base, "a")
	b := filepath.Join(base, "b")
	for _, d := range []string{a, b} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ResolveWithin([]string{a, b}, b)
	if err != nil || got != b {
		t.Fatalf("ResolveWithin = %q, %v; erwartet %q", got, err, b)
	}
}

func TestWithin(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !Within([]string{base}, filepath.Join(base, "x")) {
		t.Error("Within sollte true liefern")
	}
	if Within([]string{filepath.Join(base, "b")}, filepath.Join(base, "bc")) {
		t.Error("Within darf die Präfix-Falle nicht akzeptieren")
	}
}
