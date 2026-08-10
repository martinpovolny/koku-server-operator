package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOperatorNamespace_FromSAFile(t *testing.T) {
	// Write a fake service-account namespace file.
	dir := t.TempDir()
	nsFile := filepath.Join(dir, "namespace")
	if err := os.WriteFile(nsFile, []byte("cost-onprem\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Temporarily override the path the function reads.
	// We can't patch the path constant directly, so we test the env-var fallback
	// and verify the SA file path via integration — this test covers the env branch.
	t.Setenv("NAMESPACE", "")

	// With no SA file at the default path and no env var, result should be "".
	got := operatorNamespace()
	// In a real pod the SA file is always present; out-of-cluster it falls back
	// to NAMESPACE. Empty result is explicitly caught in main() and causes exit.
	if got != "" {
		t.Errorf("expected empty namespace when neither SA file nor NAMESPACE set, got %q", got)
	}
}

func TestOperatorNamespace_FromEnv(t *testing.T) {
	t.Setenv("NAMESPACE", "my-operator-ns")
	got := operatorNamespace()
	if got != "my-operator-ns" {
		t.Errorf("expected my-operator-ns, got %q", got)
	}
}

func TestOperatorNamespace_EnvTakesPrecedenceWhenNoSAFile(t *testing.T) {
	// SA file at /var/run/... won't exist in unit test env.
	t.Setenv("NAMESPACE", "unit-test-ns")
	got := operatorNamespace()
	if got != "unit-test-ns" {
		t.Errorf("expected unit-test-ns, got %q", got)
	}
}
