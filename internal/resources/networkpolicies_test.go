package resources

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
)

func TestGatewayNetworkPolicy(t *testing.T) {
	cfg := testCfg()
	np := GatewayNetworkPolicy(cfg)
	if np.Name != cfg.Name+"-gateway" {
		t.Errorf("Name = %q", np.Name)
	}
	assertIngressOnly(t, np)
	if got := np.Spec.PodSelector.MatchLabels[labelComponent]; got != "gateway" {
		t.Errorf("podSelector component = %q", got)
	}
	// Router + UI + monitoring rules.
	if len(np.Spec.Ingress) < 3 {
		t.Fatalf("expected ≥3 ingress rules, got %d", len(np.Spec.Ingress))
	}
	if !ruleAllowsPort(np.Spec.Ingress, envoyHTTPPort) {
		t.Errorf("missing HTTP port %d", envoyHTTPPort)
	}
	if !ruleAllowsPort(np.Spec.Ingress, envoyAdminPort) {
		t.Errorf("missing admin port %d", envoyAdminPort)
	}
}

func TestIngressNetworkPolicy_GatewayOnly(t *testing.T) {
	cfg := testCfg()
	np := IngressNetworkPolicy(cfg)
	if np.Name != cfg.Name+"-ingress" {
		t.Errorf("Name = %q", np.Name)
	}
	assertIngressOnly(t, np)
	if len(np.Spec.Ingress) != 1 {
		t.Fatalf("expected single gateway rule, got %d", len(np.Spec.Ingress))
	}
	if !peerHasComponent(np.Spec.Ingress[0], "gateway") {
		t.Error("ingress rule must allow from gateway")
	}
	if !ruleAllowsPort(np.Spec.Ingress, ingressHTTPPort) {
		t.Errorf("missing ingress HTTP port %d", ingressHTTPPort)
	}
}

func TestKruizeNetworkPolicy(t *testing.T) {
	cfg := testCfg()
	np := KruizeNetworkPolicy(cfg)
	assertIngressOnly(t, np)
	wantFrom := []string{"ros-processor", "ros-recommendation-poller", "ros-housekeeper"}
	for _, comp := range wantFrom {
		found := false
		for _, rule := range np.Spec.Ingress {
			if peerHasComponent(rule, comp) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing peer component %q", comp)
		}
	}
}

func TestRBACAPINetworkPolicy(t *testing.T) {
	cfg := testCfg()
	np := RBACAPINetworkPolicy(cfg)
	assertIngressOnly(t, np)
	wantFrom := []string{"gateway", "cost-management-api", "cost-processor", "ros-api"}
	for _, comp := range wantFrom {
		found := false
		for _, rule := range np.Spec.Ingress {
			if peerHasComponent(rule, comp) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing peer component %q", comp)
		}
	}
}

func TestKokuAPINetworkPolicy(t *testing.T) {
	const (
		kokuAPIPort     = int32(8000)
		kokuMetricsPort = int32(9000)
	)
	cfg := testCfg()
	np := KokuAPINetworkPolicy(cfg)
	assertIngressOnly(t, np)
	if !ruleAllowsPort(np.Spec.Ingress, kokuAPIPort) {
		t.Errorf("missing koku API port %d", kokuAPIPort)
	}
	if !ruleAllowsPort(np.Spec.Ingress, kokuMetricsPort) {
		t.Errorf("missing koku metrics port %d", kokuMetricsPort)
	}
	if !peerHasComponent(np.Spec.Ingress[0], "gateway") {
		t.Error("first rule should allow gateway")
	}
}

func TestCacheNetworkPolicy(t *testing.T) {
	cachePeers := []string{
		"cost-management-api",
		"cost-processor",
		"listener",
		"cost-scheduler",
		"cost-worker-celery",
		"cost-worker-priority",
		"cost-worker-summary",
		"cost-worker-ocp",
		"cost-worker-cost-model",
		"cost-worker-refresh",
		"cost-worker-hcs",
		"cost-worker-download",
		"cost-worker-subs-extraction",
		"cost-worker-subs-transmission",
		"rbac-api",
		"rbac-worker",
	}

	cfg := testCfg()
	np := CacheNetworkPolicy(cfg)
	if np.Name != cfg.Name+"-cache" {
		t.Errorf("Name = %q", np.Name)
	}
	assertIngressOnly(t, np)
	if got := np.Spec.PodSelector.MatchLabels[labelComponent]; got != "cache" {
		t.Errorf("podSelector component = %q", got)
	}
	if len(np.Spec.Ingress) != len(cachePeers) {
		t.Fatalf("ingress rules = %d, want %d", len(np.Spec.Ingress), len(cachePeers))
	}
	if !ruleAllowsPort(np.Spec.Ingress, 6379) {
		t.Errorf("missing default cache port 6379")
	}
	for _, comp := range []string{"cost-management-api", "cost-processor", "listener", "rbac-api", "rbac-worker", "cost-worker-celery", "cost-worker-summary"} {
		found := false
		for _, rule := range np.Spec.Ingress {
			if peerHasComponent(rule, comp) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing peer component %q", comp)
		}
	}

	cfgCustom := testCfg()
	cfgCustom.Spec.Cache.Port = 6380
	npCustom := CacheNetworkPolicy(cfgCustom)
	if !ruleAllowsPort(npCustom.Spec.Ingress, 6380) {
		t.Errorf("missing custom cache port 6380")
	}
}

func TestDatabaseNetworkPolicy(t *testing.T) {
	dbPeers := []string{
		"cost-management-api",
		"cost-processor",
		"rbac-api",
		"rbac-worker",
		"ros-api",
		"ros-processor",
		"ros-recommendation-poller",
		"ros-housekeeper",
		"ros-optimization",
	}

	cfg := testCfg()
	np := DatabaseNetworkPolicy(cfg)
	if np.Name != cfg.Name+"-database" {
		t.Errorf("Name = %q", np.Name)
	}
	assertIngressOnly(t, np)
	if got := np.Spec.PodSelector.MatchLabels[labelComponent]; got != "database" {
		t.Errorf("podSelector component = %q", got)
	}
	if len(np.Spec.Ingress) != len(dbPeers) {
		t.Fatalf("ingress rules = %d, want %d", len(np.Spec.Ingress), len(dbPeers))
	}
	if !ruleAllowsPort(np.Spec.Ingress, 5432) {
		t.Errorf("missing default database port 5432")
	}
	for _, comp := range []string{"cost-management-api", "cost-processor", "rbac-api", "ros-api", "ros-processor", "ros-optimization"} {
		found := false
		for _, rule := range np.Spec.Ingress {
			if peerHasComponent(rule, comp) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing peer component %q", comp)
		}
	}

	cfgCustom := testCfg()
	cfgCustom.Spec.Database.Port = 5433
	npCustom := DatabaseNetworkPolicy(cfgCustom)
	if !ruleAllowsPort(npCustom.Spec.Ingress, 5433) {
		t.Errorf("missing custom database port 5433")
	}
}

func TestROSAPINetworkPolicy_GatewayOnly(t *testing.T) {
	// Extends the smoke test in names_test.go with peer/port assertions.
	cfg := testCfg()
	np := ROSAPINetworkPolicy(cfg)
	assertIngressOnly(t, np)
	if len(np.Spec.Ingress) != 1 {
		t.Fatalf("expected single gateway rule, got %d", len(np.Spec.Ingress))
	}
	if !peerHasComponent(np.Spec.Ingress[0], "gateway") {
		t.Error("ROS API must only accept traffic from gateway")
	}
	if !ruleAllowsPort(np.Spec.Ingress, rosAPIPort) {
		t.Errorf("missing ros API port %d", rosAPIPort)
	}
}

func assertIngressOnly(t *testing.T, np *networkingv1.NetworkPolicy) {
	t.Helper()
	if len(np.Spec.PolicyTypes) != 1 || np.Spec.PolicyTypes[0] != networkingv1.PolicyTypeIngress {
		t.Errorf("PolicyTypes = %v, want [Ingress]", np.Spec.PolicyTypes)
	}
}

func peerHasComponent(rule networkingv1.NetworkPolicyIngressRule, component string) bool {
	for _, from := range rule.From {
		if from.PodSelector != nil && from.PodSelector.MatchLabels[labelComponent] == component {
			return true
		}
	}
	return false
}

func ruleAllowsPort(rules []networkingv1.NetworkPolicyIngressRule, port int32) bool {
	for _, rule := range rules {
		for _, p := range rule.Ports {
			if p.Port != nil && p.Port.IntVal == port {
				if p.Protocol == nil || *p.Protocol == corev1.ProtocolTCP {
					return true
				}
			}
		}
	}
	return false
}
