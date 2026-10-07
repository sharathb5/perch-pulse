package cli

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestResolveSPAPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want string
	}{
		{in: "/", want: "index.html"},
		{in: "", want: "index.html"},
		{in: "/assets/app.js", want: "assets/app.js"},
		{in: "assets/app.js", want: "assets/app.js"},
		{in: "/stack/prod", want: "stack/prod"},
		{in: "/./favicon.svg", want: "favicon.svg"},
	}
	for _, tc := range tests {
		if got := resolveSPAPath(tc.in); got != tc.want {
			t.Errorf("resolveSPAPath(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestPathContainsDotDot(t *testing.T) {
	t.Parallel()
	if pathContainsDotDot("/assets/app.js") {
		t.Fatal("expected false for normal path")
	}
	if !pathContainsDotDot("/../../etc/passwd") {
		t.Fatal("expected true for traversal path")
	}
	if !pathContainsDotDot("/foo/../bar") {
		t.Fatal("expected true when segment is ..")
	}
}

func TestSPAHandler_ServesAssetsAndFallsBack(t *testing.T) {
	t.Parallel()
	root := fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("spa-shell")},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log(1)")},
		"favicon.svg":   &fstest.MapFile{Data: []byte("<svg/>")},
	}
	h := spaHandler(root)

	t.Run("root", func(t *testing.T) {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
		if rr.Code != http.StatusOK || rr.Body.String() != "spa-shell" {
			t.Fatalf("status=%d body=%q", rr.Code, rr.Body.String())
		}
	})

	t.Run("asset", func(t *testing.T) {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
		if rr.Code != http.StatusOK || rr.Body.String() != "console.log(1)" {
			t.Fatalf("status=%d body=%q", rr.Code, rr.Body.String())
		}
	})

	t.Run("missing route falls back to index", func(t *testing.T) {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/stack/dev/web", nil))
		if rr.Code != http.StatusOK || rr.Body.String() != "spa-shell" {
			t.Fatalf("status=%d body=%q", rr.Code, rr.Body.String())
		}
	})

	t.Run("rejects dot-dot", func(t *testing.T) {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/../../secret", nil))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want 400; body=%q", rr.Code, rr.Body.String())
		}
		if strings.Contains(rr.Body.String(), "spa-shell") {
			t.Fatalf("traversal response should not serve SPA body: %q", rr.Body.String())
		}
	})

	t.Run("method not allowed", func(t *testing.T) {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/", nil))
		if rr.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status=%d want 405", rr.Code)
		}
	})
}

func TestOpenSPAFile_DirectoryFallsBack(t *testing.T) {
	t.Parallel()
	root := fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("spa-shell")},
		"assets/app.js": &fstest.MapFile{Data: []byte("js")},
		"assets":        &fstest.MapFile{Mode: fs.ModeDir},
	}
	f, err := openSPAFile(root, "assets")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if st.Name() != "index.html" {
		t.Fatalf("got %q want index.html fallback", st.Name())
	}
}
