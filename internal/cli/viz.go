package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yashg4509/perch/internal/config"
	"github.com/yashg4509/perch/internal/credentials"
	"github.com/yashg4509/perch/internal/customlogs"
	"github.com/yashg4509/perch/internal/graph"
	"github.com/yashg4509/perch/internal/provider"
	"github.com/yashg4509/perch/internal/stacklogs"
	"github.com/yashg4509/perch/internal/stackstatus"
	webdist "github.com/yashg4509/perch/web"
)

const logsRunTimeout = 8 * time.Second

func newVizCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "viz",
		Short: "Start local web UI for stack visualization",
		RunE:  runViz,
	}
	cmd.Flags().Int("port", 3131, "HTTP listen port")
	return cmd
}

func runViz(cmd *cobra.Command, args []string) error {
	_ = args
	port, err := cmd.Flags().GetInt("port")
	if err != nil {
		return err
	}
	defaultEnv, err := cmd.Flags().GetString("env")
	if err != nil {
		return err
	}
	if port <= 0 || port > 65535 {
		return fmt.Errorf("invalid port %d", port)
	}

	_, _, _, err = loadStackFromWD()
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "# → Building graph from perch.yaml...")
	addr := net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port))
	baseURL := fmt.Sprintf("http://localhost:%d", port)
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "# → Perch UI running at %s\n", baseURL)
	_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "# → Opening browser...")

	go openBrowser(baseURL + "/")

	// go:embed all:dist exposes paths as dist/index.html, dist/assets/..., not index.html at root.
	uiFS, err := fs.Sub(webdist.Dist, "dist")
	if err != nil {
		return fmt.Errorf("web UI embed: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/graph", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		serveGraphJSON(w, r, defaultEnv)
	})
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		serveStatusJSON(w, r, defaultEnv)
	})
	mux.HandleFunc("/api/logs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		serveLogsJSON(w, r, defaultEnv)
	})
	mux.HandleFunc("/api/credentials", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		serveCredentialsPost(w, r)
	})
	// Milestone A: read-only Pulse API (optional relative to core Perch).
	if err := registerPulseAPI(mux, defaultEnv); err != nil {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "# →", pulseAPIMountError(err))
	} else {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "# → Pulse read-only API mounted at /api/pulse/*")
	}
	mux.Handle("/", spaHandler(uiFS))

	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	return srv.ListenAndServe()
}

func loadStackFromWD() (*config.Config, *provider.Registry, string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, nil, "", err
	}
	perchPath, err := config.FindPerchYAML(wd)
	if err != nil {
		return nil, nil, "", err
	}
	raw, err := os.ReadFile(perchPath)
	if err != nil {
		return nil, nil, "", fmt.Errorf("read config: %w", err)
	}
	cfg, err := config.Load(raw)
	if err != nil {
		return nil, nil, "", err
	}
	root := filepath.Dir(perchPath)
	reg, err := loadRegistryForProject(root)
	if err != nil {
		return nil, nil, "", err
	}
	return cfg, reg, perchPath, nil
}

func envFromRequest(r *http.Request, defaultEnv string) string {
	q := r.URL.Query().Get("env")
	if strings.TrimSpace(q) == "" {
		return defaultEnv
	}
	return q
}

func isBadEnvErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "unknown environment") ||
		strings.Contains(s, "environment name is required")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	_ = enc.Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func serveGraphJSON(w http.ResponseWriter, r *http.Request, defaultEnv string) {
	env := envFromRequest(r, defaultEnv)
	cfg, reg, _, err := loadStackFromWD()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	g, err := graph.Build(cfg, reg, env)
	if err != nil {
		if isBadEnvErr(err) {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rep := graph.NewJSONReport(g, reg)
	writeJSON(w, http.StatusOK, rep)
}

type credentialsPostRequest struct {
	Key   string `json:"key"`
	Token string `json:"token"`
}

// serveCredentialsPost saves a provider API token to ~/.perch/credentials.
// Plain HTTP is acceptable: perch viz listens only on 127.0.0.1, so this endpoint
// is not reachable from other machines on the network.
func serveCredentialsPost(w http.ResponseWriter, r *http.Request) {
	const maxBody = 16 << 10
	var req credentialsPostRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	key := strings.TrimSpace(req.Key)
	token := strings.TrimSpace(req.Token)
	if key == "" || token == "" {
		writeJSONError(w, http.StatusBadRequest, "key and token are required")
		return
	}

	_, reg, _, err := loadStackFromWD()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !credentialKeyKnown(reg, key) {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("unknown credentials key %q", key))
		return
	}

	store := credentials.NewStore()
	if err := store.Set(key, token); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func credentialKeyKnown(reg *provider.Registry, key string) bool {
	if reg == nil || strings.TrimSpace(key) == "" {
		return false
	}
	for _, spec := range reg.ByName {
		if spec != nil && strings.TrimSpace(spec.Credentials.Key) == key {
			return true
		}
	}
	return false
}

func serveStatusJSON(w http.ResponseWriter, r *http.Request, defaultEnv string) {
	env := envFromRequest(r, defaultEnv)
	cfg, reg, perchPath, err := loadStackFromWD()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ctx := context.Background()
	rep, err := stackstatus.Collect(ctx, cfg, env, reg, loadCollectOptions(perchPath))
	if err != nil {
		if isBadEnvErr(err) {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

type logsResponse struct {
	StdoutLines     []string                   `json:"stdout_lines,omitempty"`
	StderrLines     []string                   `json:"stderr_lines,omitempty"`
	ExitCode        int                        `json:"exit_code"`
	Truncated       bool                       `json:"truncated"`
	TimedOut        bool                       `json:"timed_out"`
	RunError        string                     `json:"run_error,omitempty"`
	Source          string                     `json:"source,omitempty"`
	SetupHint       string                     `json:"setup_hint,omitempty"`
	StrategiesTried []stacklogs.StrategyResult `json:"strategies_tried,omitempty"`
}

func serveLogsJSON(w http.ResponseWriter, r *http.Request, defaultEnv string) {
	env := envFromRequest(r, defaultEnv)
	nodeName := strings.TrimSpace(r.URL.Query().Get("node"))
	if nodeName == "" {
		writeJSONError(w, http.StatusBadRequest, "node query parameter is required")
		return
	}

	cfg, reg, _, err := loadStackFromWD()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	nodes, ok := cfg.Environments[env]
	if !ok {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("unknown environment %q", env))
		return
	}
	n, ok := nodes[nodeName]
	if !ok {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("unknown node %q for environment %q", nodeName, env))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), logsRunTimeout)
	defer cancel()

	if n.Provider == "custom" {
		if strings.TrimSpace(n.Logs) == "" {
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("node %q has no logs command", nodeName))
			return
		}
		res, err := customlogs.Run(ctx, n.Logs)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, logsResponse{
			StdoutLines: res.StdoutLines,
			StderrLines: res.StderrLines,
			ExitCode:    res.ExitCode,
			Truncated:   res.Truncated,
			TimedOut:    res.TimedOut,
			RunError:    res.RunError,
			Source:      "custom",
		})
		return
	}

	logRes, err := stacklogs.Resolve(ctx, nodeName, n, reg)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp := logsResponse{
		StdoutLines:     logRes.Lines,
		ExitCode:        0,
		Truncated:       logRes.Truncated,
		Source:          logRes.Source,
		SetupHint:       logRes.SetupHint,
		StrategiesTried: logRes.StrategiesTried,
	}
	if logRes.Source == "none" && len(logRes.Lines) == 0 {
		resp.ExitCode = 0
	}
	writeJSON(w, http.StatusOK, resp)
}

func spaHandler(root fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		// Reject ".." elements in the raw URL path (same precaution as http.ServeFileFS).
		if pathContainsDotDot(r.URL.Path) {
			http.Error(w, "invalid URL path", http.StatusBadRequest)
			return
		}
		name := resolveSPAPath(r.URL.Path)
		serveEmbeddedFile(w, r, root, name)
	})
}

// resolveSPAPath maps a request URL path to a relative name safe for fs.FS.Open.
// Unknown client routes fall through to index.html (SPA).
func resolveSPAPath(requestPath string) string {
	cleaned := path.Clean("/" + strings.TrimPrefix(requestPath, "/"))
	if cleaned == "/" {
		return "index.html"
	}
	rel := strings.TrimPrefix(cleaned, "/")
	if !fs.ValidPath(rel) {
		return "index.html"
	}
	return rel
}

func pathContainsDotDot(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

// serveEmbeddedFile opens name from root after fs.ValidPath checks and streams it
// with ServeContent. This avoids passing request-tainted paths into ServeFileFS
// (gosec G703) while keeping the SPA fallback for missing assets.
func serveEmbeddedFile(w http.ResponseWriter, r *http.Request, root fs.FS, name string) {
	if !fs.ValidPath(name) {
		name = "index.html"
	}
	f, err := openSPAFile(root, name)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, path.Base(stat.Name()), stat.ModTime(), rs)
}

func openSPAFile(root fs.FS, name string) (fs.File, error) {
	f, err := root.Open(name)
	if err == nil {
		st, sterr := f.Stat()
		if sterr == nil && !st.IsDir() {
			return f, nil
		}
		_ = f.Close()
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return root.Open("index.html")
}

// openBrowser launches the default browser for a perch viz URL.
// Only http(s) URLs with host localhost or 127.0.0.1 are allowed (no shell, no remote hosts).
func openBrowser(rawURL string) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return
	}
	host := u.Hostname()
	if host != "localhost" && host != "127.0.0.1" {
		return
	}
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return
		}
	}
	safe := u.String()

	switch runtime.GOOS {
	case "darwin":
		// #nosec G204 -- argv[0] is constant "open"; argv[1] is a validated local http(s) URL only.
		_ = exec.Command("open", safe).Start()
	case "windows":
		// #nosec G204 -- fixed Windows shell invocation; URL validated as local-only above.
		_ = exec.Command("cmd", "/c", "start", "", safe).Start()
	default:
		// #nosec G204 -- argv[0] is constant "xdg-open"; argv[1] is a validated local http(s) URL only.
		_ = exec.Command("xdg-open", safe).Start()
	}
}
