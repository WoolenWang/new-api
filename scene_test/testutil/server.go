// Package testutil provides utilities for integration testing of the NewAPI service.
// It manages the lifecycle of the test server, including compilation, startup, and teardown.
package testutil

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

// TestServer represents a running instance of the NewAPI server for testing.
type TestServer struct {
	// BaseURL is the URL where the test server is listening (e.g., "http://localhost:12345").
	BaseURL string

	// Port is the actual port the server is listening on.
	Port int

	// DataDir is the temporary directory for test data (SQLite DB, etc.).
	DataDir string

	// AdminToken is the root user's access token for API calls.
	AdminToken string

	cmd      *exec.Cmd
	cancelFn context.CancelFunc
	exePath  string
	stdout   io.ReadCloser
	stderr   io.ReadCloser
	wg       sync.WaitGroup
	mu       sync.Mutex
	logs     []string
}

// ServerConfig holds configuration options for starting a test server.
type ServerConfig struct {
	// ProjectRoot is the root directory of the NewAPI project.
	// If empty, it will be auto-detected.
	ProjectRoot string

	// UseInMemoryDB uses SQLite in-memory mode if true (default: true).
	UseInMemoryDB bool

	// CustomEnv allows setting additional environment variables.
	CustomEnv map[string]string

	// Verbose enables verbose logging of server output.
	Verbose bool

	// StartupTimeout is the maximum time to wait for server startup (default: 60s).
	StartupTimeout time.Duration
}

// DefaultConfig returns a ServerConfig with sensible defaults.
func DefaultConfig() ServerConfig {
	return ServerConfig{
		UseInMemoryDB:  true,
		StartupTimeout: 60 * time.Second,
		Verbose:        false,
	}
}

// findProjectRoot attempts to locate the project root directory by looking for go.mod.
func findProjectRoot() (string, error) {
	// Start from the current working directory
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}

	// Walk up the directory tree looking for go.mod
	for {
		goModPath := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(goModPath); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root
			break
		}
		dir = parent
	}

	return "", fmt.Errorf("could not find project root (no go.mod found)")
}

// findFreePort finds an available TCP port.
func findFreePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("failed to find free port: %w", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

// executableName returns the appropriate executable name for the current OS.
func executableName() string {
	if runtime.GOOS == "windows" {
		return "new-api-test.exe"
	}
	return "new-api-test"
}

var (
	compileOnce   sync.Once
	compiledPath  string
	compiledError error
)

// CompileTestServer compiles the NewAPI server for testing.
// It returns the path to the compiled executable.
func CompileTestServer(projectRoot string) (string, error) {
	compileOnce.Do(func() {
		// Create a dedicated temporary directory for the compiled binary to
		// avoid cross-package races when tests are run with `./scene_test/...`.
		tmpDir, err := os.MkdirTemp("", "newapi-test-*")
		if err != nil {
			compiledError = fmt.Errorf("failed to create temp dir for test server: %w", err)
			return
		}

		exePath := filepath.Join(tmpDir, executableName())

		// Compile the main package into the temporary path.
		cmd := exec.Command("go", "build", "-o", exePath, ".")
		cmd.Dir = projectRoot
		cmd.Env = append(os.Environ(), "CGO_ENABLED=1")

		output, err := cmd.CombinedOutput()
		if err != nil {
			compiledError = fmt.Errorf("failed to compile: %w\nOutput: %s", err, string(output))
			return
		}

		compiledPath = exePath
	})

	return compiledPath, compiledError
}

// StartServer starts a new test server instance with the given configuration.
func StartServer(cfg ServerConfig) (*TestServer, error) {
	// Find project root if not specified
	projectRoot := cfg.ProjectRoot
	if projectRoot == "" {
		var err error
		projectRoot, err = findProjectRoot()
		if err != nil {
			return nil, fmt.Errorf("failed to find project root: %w", err)
		}
	}

	// Compile the server
	exePath, err := CompileTestServer(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to compile server: %w", err)
	}

	// Find a free port
	port, err := findFreePort()
	if err != nil {
		return nil, fmt.Errorf("failed to find free port: %w", err)
	}

	// Create temporary data directory
	dataDir, err := os.MkdirTemp("", "newapi-test-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}

	// Create a cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	// Build the command
	cmd := exec.CommandContext(ctx, exePath)
	// Run from the temp data directory for SQLite isolation
	// The server will create one-api.db in its working directory
	cmd.Dir = dataDir

	// Set up environment variables
	env := os.Environ()

	// Filter out existing conflicting variables
	filteredEnv := make([]string, 0, len(env))
	for _, e := range env {
		key := strings.SplitN(e, "=", 2)[0]
		switch key {
		case "PORT", "SQL_DSN", "LOG_SQL_DSN", "GIN_MODE", "SESSION_SECRET", "CRYPTO_SECRET":
			continue
		default:
			filteredEnv = append(filteredEnv, e)
		}
	}

	// Add test-specific environment variables
	testEnv := map[string]string{
		"PORT":           fmt.Sprintf("%d", port),
		"GIN_MODE":       "release",
		"SESSION_SECRET": "test-session-secret-12345",
		"CRYPTO_SECRET":  "test-crypto-secret-12345",
	}

	// Configure database
	if cfg.UseInMemoryDB {
		// The NewAPI uses "local" prefix to trigger SQLite mode
		// We do NOT set SQL_DSN to let the system use the default SQLite path
		// Instead, we need to ensure the working directory has proper SQLite setup
		// Note: The actual SQLite file will be created at common.SQLitePath (one-api.db)
		// For isolation, we run from a temp directory

		// Don't set SQL_DSN - let it use default SQLite
		// The system will create one-api.db in the working directory
	}

	// Add custom environment variables
	for k, v := range cfg.CustomEnv {
		testEnv[k] = v
	}

	// Build final environment
	for k, v := range testEnv {
		filteredEnv = append(filteredEnv, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = filteredEnv

	// Capture stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		os.RemoveAll(dataDir)
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		os.RemoveAll(dataDir)
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	server := &TestServer{
		BaseURL:  fmt.Sprintf("http://127.0.0.1:%d", port),
		Port:     port,
		DataDir:  dataDir,
		cmd:      cmd,
		cancelFn: cancel,
		exePath:  exePath,
		stdout:   stdout,
		stderr:   stderr,
	}

	// Start log collectors
	server.wg.Add(2)
	go server.collectLogs(stdout, "stdout", cfg.Verbose)
	go server.collectLogs(stderr, "stderr", cfg.Verbose)

	// Start the server process
	if err := cmd.Start(); err != nil {
		cancel()
		os.RemoveAll(dataDir)
		return nil, fmt.Errorf("failed to start server: %w", err)
	}

	// Wait for server to become ready
	timeout := cfg.StartupTimeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	if err := server.waitForReady(timeout); err != nil {
		server.Stop()
		return nil, fmt.Errorf("server failed to become ready: %w", err)
	}

	return server, nil
}

// collectLogs reads from a pipe and stores log lines.
func (s *TestServer) collectLogs(pipe io.ReadCloser, source string, verbose bool) {
	defer s.wg.Done()
	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		line := scanner.Text()
		s.mu.Lock()
		s.logs = append(s.logs, fmt.Sprintf("[%s] %s", source, line))
		s.mu.Unlock()
		if verbose {
			fmt.Printf("[TestServer:%s] %s\n", source, line)
		}
	}
}

// waitForReady polls the server until it responds to health checks.
func (s *TestServer) waitForReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}

	// Regex to extract admin token from log output
	tokenRegex := regexp.MustCompile(`access_token:\s*(\S+)`)

	for time.Now().Before(deadline) {
		// Check if process has exited
		select {
		case <-time.After(500 * time.Millisecond):
		default:
		}

		// Try to connect to the status endpoint
		resp, err := client.Get(s.BaseURL + "/api/status")
		if err != nil {
			continue
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			// Server is ready, try to extract admin token from logs
			s.mu.Lock()
			for _, log := range s.logs {
				if matches := tokenRegex.FindStringSubmatch(log); len(matches) > 1 {
					s.AdminToken = matches[1]
					break
				}
			}
			s.mu.Unlock()
			return nil
		}
	}

	return fmt.Errorf("server did not become ready within %v", timeout)
}

// Stop gracefully shuts down the test server and cleans up resources.
func (s *TestServer) Stop() error {
	if s.cancelFn != nil {
		s.cancelFn()
	}

	// Wait for the process to exit
	if s.cmd != nil && s.cmd.Process != nil {
		done := make(chan error, 1)
		go func() {
			done <- s.cmd.Wait()
		}()

		select {
		case <-done:
			// Process exited normally
		case <-time.After(10 * time.Second):
			// Force kill if it doesn't exit gracefully
			s.cmd.Process.Kill()
		}
	}

	// Wait for log collectors to finish
	s.wg.Wait()

	// Clean up temporary data directory
	if s.DataDir != "" {
		os.RemoveAll(s.DataDir)
	}

	return nil
}

// GetLogs returns all captured log lines from the server.
func (s *TestServer) GetLogs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]string, len(s.logs))
	copy(result, s.logs)
	return result
}

// HealthCheck performs a health check against the server.
func (s *TestServer) HealthCheck() error {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(s.BaseURL + "/api/status")
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned status %d", resp.StatusCode)
	}
	return nil
}

// CleanupTestExecutable removes the compiled test executable and its temp directory.
func CleanupTestExecutable(exePath string) error {
	if exePath == "" {
		return nil
	}
	dir := filepath.Dir(exePath)
	// Remove the entire temp directory tree; ignore if it no longer exists.
	if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// FindProjectRoot is a public wrapper for findProjectRoot, allowing other packages to use it.
func FindProjectRoot() (string, error) {
	return findProjectRoot()
}

// StartTestServer is a convenience helper used by higher-level
// scene tests. It starts a NewAPI server with default test
// configuration and an auto-detected project root.
func StartTestServer() (*TestServer, error) {
	cfg := DefaultConfig()

	projectRoot, err := findProjectRoot()
	if err != nil {
		return nil, fmt.Errorf("failed to find project root for test server: %w", err)
	}
	cfg.ProjectRoot = projectRoot

	return StartServer(cfg)
}
