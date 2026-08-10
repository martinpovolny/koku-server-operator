package main

import (
	"testing"
)

// TestOperatorNamespace_NoSAFileNoEnv_ReturnsEmpty covers the out-of-cluster
// path where neither the SA-token file exists (unit tests run outside a pod)
// nor the NAMESPACE env var is set. main() catches this and exits with an
// error message rather than silently watching all namespaces.
func TestOperatorNamespace_NoSAFileNoEnv_ReturnsEmpty(t *testing.T) {
	t.Setenv("NAMESPACE", "")
	if got := operatorNamespace(); got != "" {
		t.Errorf("expected empty when no SA file and no NAMESPACE, got %q", got)
	}
}

func TestOperatorNamespace_FromEnv(t *testing.T) {
	t.Setenv("NAMESPACE", "my-operator-ns")
	if got := operatorNamespace(); got != "my-operator-ns" {
		t.Errorf("expected my-operator-ns, got %q", got)
	}
}

func TestOperatorNamespace_EnvNotUsedWhenEmpty(t *testing.T) {
	t.Setenv("NAMESPACE", "")
	if got := operatorNamespace(); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}
