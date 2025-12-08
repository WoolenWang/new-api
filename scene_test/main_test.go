// Package scene_test contains integration tests for the NewAPI service.
//
// Test Framework Overview:
// ========================
// This package implements an automated integration testing framework based on Go's
// testing package. The framework manages the complete lifecycle of the test server:
//
// 1. Compilation: Compiles the NewAPI main package into a test executable
// 2. Startup: Launches the server with test-specific configuration (in-memory DB, random port)
// 3. Execution: Runs all test suites against the live server
// 4. Teardown: Gracefully shuts down the server and cleans up resources
//
// Usage:
// ======
// Run all tests from the project root:
//
//	go test -v ./scene_test/...
//
// Run specific test suite:
//
//	go test -v ./scene_test/new-api-data-plane/routing-authorization/...
//
// Run with verbose server logs:
//
//	go test -v ./scene_test/... -args -verbose
package scene_test

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/scene_test/testutil"
)

var (
	// TestServer is the shared test server instance.
	// Each test package should use this instance to avoid multiple compilations.
	TestServer *testutil.TestServer

	// ProjectRoot is the root directory of the NewAPI project.
	ProjectRoot string

	// verbose enables verbose server logging.
	verbose bool
)

func init() {
	flag.BoolVar(&verbose, "verbose", false, "Enable verbose server logging")
}

// TestMain is the entry point for all tests in this package.
// It handles:
// 1. Compilation of the test server executable
// 2. Server lifecycle management
// 3. Cleanup after all tests complete
func TestMain(m *testing.M) {
	// Parse command-line flags
	flag.Parse()

	// Find project root
	var err error
	ProjectRoot, err = findProjectRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to find project root: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Project root: %s\n", ProjectRoot)

	// Compile the test server once for all tests
	fmt.Println("Compiling test server...")
	exePath, err := testutil.CompileTestServer(ProjectRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to compile test server: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Test server compiled: %s\n", exePath)

	// Note: We don't start a global server here because each test suite
	// may need different configurations. Instead, we provide helper functions
	// that test suites can use to start their own server instances.

	// Run all tests
	exitCode := m.Run()

	// Cleanup: Remove the compiled executable
	fmt.Println("Cleaning up test executable...")
	if err := testutil.CleanupTestExecutable(ProjectRoot); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to cleanup test executable: %v\n", err)
	}

	os.Exit(exitCode)
}

// findProjectRoot locates the project root directory by looking for go.mod.
func findProjectRoot() (string, error) {
	// Start from the test directory
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	// Walk up looking for go.mod
	for {
		goModPath := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(goModPath); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", fmt.Errorf("could not find project root (no go.mod found)")
}

// TestFrameworkSetup is a basic test to verify the test framework is working.
func TestFrameworkSetup(t *testing.T) {
	t.Run("ProjectRootExists", func(t *testing.T) {
		if ProjectRoot == "" {
			t.Fatal("ProjectRoot is empty")
		}

		goModPath := filepath.Join(ProjectRoot, "go.mod")
		if _, err := os.Stat(goModPath); os.IsNotExist(err) {
			t.Fatalf("go.mod not found at %s", goModPath)
		}
		t.Logf("Project root verified: %s", ProjectRoot)
	})

	t.Run("TestServerCompilation", func(t *testing.T) {
		exePath := filepath.Join(ProjectRoot, "scene_test", "new-api-test.exe")
		if _, err := os.Stat(exePath); os.IsNotExist(err) {
			// Try without .exe for non-Windows
			exePath = filepath.Join(ProjectRoot, "scene_test", "new-api-test")
			if _, err := os.Stat(exePath); os.IsNotExist(err) {
				t.Fatal("Test server executable not found")
			}
		}
		t.Logf("Test executable found: %s", exePath)
	})
}

// TestServerStartStop verifies that we can start and stop a test server.
func TestServerStartStop(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping server start/stop test in short mode")
	}

	cfg := testutil.DefaultConfig()
	cfg.ProjectRoot = ProjectRoot
	cfg.Verbose = verbose

	t.Log("Starting test server...")
	server, err := testutil.StartServer(cfg)
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	// Ensure cleanup happens
	defer func() {
		t.Log("Stopping test server...")
		if err := server.Stop(); err != nil {
			t.Errorf("Failed to stop server: %v", err)
		}
	}()

	t.Logf("Server started at %s", server.BaseURL)

	// Verify health check
	t.Log("Performing health check...")
	if err := server.HealthCheck(); err != nil {
		// Print server logs for debugging
		t.Log("Server logs:")
		for _, log := range server.GetLogs() {
			t.Log(log)
		}
		t.Fatalf("Health check failed: %v", err)
	}

	t.Log("Server health check passed")

	// Test API client
	client := testutil.NewAPIClient(server)
	status, err := client.GetStatus()
	if err != nil {
		t.Fatalf("Failed to get status: %v", err)
	}

	if version, ok := status["version"]; ok {
		t.Logf("Server version: %v", version)
	}

	t.Log("Test server start/stop test passed")
}

// StartTestServer is a helper function for test suites to start a server.
// Returns the server instance and a cleanup function.
func StartTestServer(t *testing.T) (*testutil.TestServer, func()) {
	t.Helper()

	cfg := testutil.DefaultConfig()
	cfg.ProjectRoot = ProjectRoot
	cfg.Verbose = verbose

	server, err := testutil.StartServer(cfg)
	if err != nil {
		t.Fatalf("Failed to start test server: %v", err)
	}

	cleanup := func() {
		if err := server.Stop(); err != nil {
			t.Errorf("Failed to stop test server: %v", err)
		}
	}

	return server, cleanup
}

// StartTestServerWithConfig starts a server with custom configuration.
func StartTestServerWithConfig(t *testing.T, cfg testutil.ServerConfig) (*testutil.TestServer, func()) {
	t.Helper()

	if cfg.ProjectRoot == "" {
		cfg.ProjectRoot = ProjectRoot
	}

	server, err := testutil.StartServer(cfg)
	if err != nil {
		t.Fatalf("Failed to start test server: %v", err)
	}

	cleanup := func() {
		if err := server.Stop(); err != nil {
			t.Errorf("Failed to stop test server: %v", err)
		}
	}

	return server, cleanup
}
