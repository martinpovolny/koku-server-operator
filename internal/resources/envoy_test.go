package resources

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	costv1alpha1 "github.com/project-koku/koku-service-operator/api/v1alpha1"
)

func testCfg() *costv1alpha1.CostManagementServiceConfig {
	return &costv1alpha1.CostManagementServiceConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cost-management", Namespace: "cost-onprem"},
		Spec: costv1alpha1.CostManagementServiceConfigSpec{
			Auth: costv1alpha1.AuthConfig{
				Keycloak: costv1alpha1.KeycloakSpec{
					URL:       "https://keycloak.keycloak.svc.cluster.local",
					Realm:     "kubernetes",
					Audiences: []string{"cost-management-operator", "cost-management-ui"},
				},
				Envoy: costv1alpha1.EnvoySpec{
					Replicas: 2,
					Image: costv1alpha1.ImageSpec{
						Repository: "registry.redhat.io/openshift-service-mesh/proxyv2-rhel9",
						Tag:        "2.6",
					},
				},
			},
		},
	}
}

func TestKeycloakIssuerAndJWKS(t *testing.T) {
	cfg := testCfg()
	wantIssuer := "https://keycloak.keycloak.svc.cluster.local/realms/kubernetes"
	if got := KeycloakIssuerURL(cfg); got != wantIssuer {
		t.Errorf("KeycloakIssuerURL = %q, want %q", got, wantIssuer)
	}
	wantJWKS := wantIssuer + "/protocol/openid-connect/certs"
	if got := KeycloakJWKSURL(cfg); got != wantJWKS {
		t.Errorf("KeycloakJWKSURL = %q, want %q", got, wantJWKS)
	}
}

func TestKeycloakIssuerURLOverrideKeepsInClusterJWKS(t *testing.T) {
	cfg := testCfg()
	cfg.Spec.Auth.Keycloak.URL = "http://keycloak-service.keycloak.svc.cluster.local:8080"
	cfg.Spec.Auth.Keycloak.IssuerURL = "https://keycloak.apps.example.com"
	cfg.Spec.Auth.Keycloak.Realm = "kubernetes"

	wantIssuer := "https://keycloak.apps.example.com/realms/kubernetes"
	if got := KeycloakIssuerURL(cfg); got != wantIssuer {
		t.Errorf("KeycloakIssuerURL = %q, want %q", got, wantIssuer)
	}
	wantJWKS := "http://keycloak-service.keycloak.svc.cluster.local:8080/realms/kubernetes/protocol/openid-connect/certs"
	if got := KeycloakJWKSURL(cfg); got != wantJWKS {
		t.Errorf("KeycloakJWKSURL = %q, want %q", got, wantJWKS)
	}

	yaml := EnvoyYAML(cfg)
	if !strings.Contains(yaml, "issuer: "+wantIssuer) {
		t.Errorf("EnvoyYAML missing issuer %q", wantIssuer)
	}
	if !strings.Contains(yaml, "uri: "+wantJWKS) {
		t.Errorf("EnvoyYAML missing JWKS uri %q", wantJWKS)
	}
	// JWKS cluster must target the in-cluster Service, not the public hostname.
	if !strings.Contains(yaml, "address: keycloak-service.keycloak.svc.cluster.local") {
		t.Error("EnvoyYAML JWKS cluster should use in-cluster Keycloak Service host")
	}
	if strings.Contains(yaml, "transport_socket:") {
		t.Error("in-cluster http JWKS should not enable upstream TLS")
	}

	// Full issuer override (includes /realms/) is used as-is.
	cfg.Spec.Auth.Keycloak.IssuerURL = "https://keycloak.apps.example.com/realms/custom"
	if got := KeycloakIssuerURL(cfg); got != "https://keycloak.apps.example.com/realms/custom" {
		t.Errorf("full IssuerURL = %q", got)
	}
}

func TestKeycloakDefaults(t *testing.T) {
	cfg := &costv1alpha1.CostManagementServiceConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "ns"},
	}
	if got := KeycloakURL(cfg); got != defaultKeycloakURL {
		t.Errorf("KeycloakURL default = %q, want %q", got, defaultKeycloakURL)
	}
	if got := KeycloakRealm(cfg); got != defaultKeycloakRealm {
		t.Errorf("KeycloakRealm default = %q, want %q", got, defaultKeycloakRealm)
	}
	aud := KeycloakAudiences(cfg)
	if len(aud) != 2 || aud[0] != "cost-management-operator" {
		t.Errorf("KeycloakAudiences default = %v", aud)
	}
}

func TestEnvoyYAMLContainsIssuerAudiencesAndKokuCluster(t *testing.T) {
	cfg := testCfg()
	yaml := EnvoyYAML(cfg)

	checks := []string{
		"issuer: https://keycloak.keycloak.svc.cluster.local/realms/kubernetes",
		"uri: https://keycloak.keycloak.svc.cluster.local/realms/kubernetes/protocol/openid-connect/certs",
		"- cost-management-operator",
		"- cost-management-ui",
		"address: cost-management-koku-api.cost-onprem.svc.cluster.local",
		"port_value: 8000",
		"X-Rh-Identity",
		"X-Bearer-Token",
		"address: keycloak.keycloak.svc.cluster.local",
		"port_value: 443",
		"transport_socket:",
	}
	for _, want := range checks {
		if !strings.Contains(yaml, want) {
			t.Errorf("EnvoyYAML missing %q", want)
		}
	}
	// Backend ports must match ROS Service (8000) and RBAC Service (8080).
	rosIdx := strings.Index(yaml, "name: ros-api-backend")
	rbacIdx := strings.Index(yaml, "name: rbac-api-backend")
	kokuIdx := strings.Index(yaml, "name: koku-api-backend")
	if rosIdx < 0 || rbacIdx < 0 || kokuIdx < 0 {
		t.Fatal("missing ros/rbac/koku backend clusters")
	}
	rosBlock := yaml[rosIdx:kokuIdx]
	if !strings.Contains(rosBlock, "port_value: 8000") {
		t.Error("ros-api-backend should use port 8000")
	}
	rbacEnd := strings.Index(yaml[rbacIdx+1:], "\n  - name:")
	rbacBlock := yaml[rbacIdx:]
	if rbacEnd >= 0 {
		rbacBlock = yaml[rbacIdx : rbacIdx+1+rbacEnd]
	}
	if !strings.Contains(rbacBlock, "port_value: 8080") {
		t.Error("rbac-api-backend should use port 8080")
	}
	for _, tok := range []string{"__HTTP_PORT__", "__ISSUER__", "__LUA__", "__KOKU_HOST__", "__KC_TLS__"} {
		if strings.Contains(yaml, tok) {
			t.Errorf("EnvoyYAML left unsubstituted token %q", tok)
		}
	}
}

func TestEnvoyYAMLHTTPKeycloakOmitsTLS(t *testing.T) {
	cfg := testCfg()
	cfg.Spec.Auth.Keycloak.URL = "http://keycloak.keycloak.svc.cluster.local:8080"
	yaml := EnvoyYAML(cfg)
	if strings.Contains(yaml, "transport_socket:") {
		t.Error("expected no TLS transport_socket for http:// Keycloak")
	}
	if !strings.Contains(yaml, "port_value: 8080") {
		t.Error("expected Keycloak port 8080")
	}
}

func TestEnvoyResourceNames(t *testing.T) {
	cfg := testCfg()
	cm := EnvoyConfigMap(cfg)
	if cm.Name != "cost-management-gateway-envoy-config" {
		t.Errorf("ConfigMap name = %q", cm.Name)
	}
	svc := EnvoyService(cfg)
	if svc.Name != "cost-management-gateway" {
		t.Errorf("Service name = %q", svc.Name)
	}
	if len(svc.Spec.Ports) != 2 || svc.Spec.Ports[0].Port != 80 {
		t.Errorf("Service ports = %+v", svc.Spec.Ports)
	}
	d := EnvoyDeployment(cfg)
	if d.Name != "cost-management-gateway" {
		t.Errorf("Deployment name = %q", d.Name)
	}
	if d.Spec.Template.Spec.Containers[0].Image != "registry.redhat.io/openshift-service-mesh/proxyv2-rhel9:2.6" {
		t.Errorf("image = %q", d.Spec.Template.Spec.Containers[0].Image)
	}
	if len(d.Spec.Template.Spec.InitContainers) != 1 || d.Spec.Template.Spec.InitContainers[0].Name != "prepare-ca-bundle" {
		t.Error("expected CA combine init container")
	}
}

// TestEnvoyDeploymentHasConfigHash verifies that EnvoyDeployment includes a
// content hash of the ConfigMap in the pod template annotations. Without this,
// Envoy pods are never restarted when the ConfigMap changes (e.g., OIDC URL
// update), so the gateway keeps running with stale JWT configuration.
func TestEnvoyDeploymentHasConfigHash(t *testing.T) {
	cfg := testCfg()
	cm := EnvoyConfigMap(cfg)
	dep := EnvoyDeployment(cfg)

	const hashAnnotation = "koku.costmanagement.io/envoy-config-hash"
	hash, ok := dep.Spec.Template.Annotations[hashAnnotation]
	if !ok {
		t.Fatalf("EnvoyDeployment pod template missing annotation %q — "+
			"ConfigMap changes will not trigger pod restarts", hashAnnotation)
	}
	if hash == "" {
		t.Fatalf("annotation %q is empty", hashAnnotation)
	}

	// Changing the ConfigMap content must change the hash.
	cfg2 := testCfg()
	cfg2.Spec.Auth.Keycloak.URL = "https://other-keycloak.example.com"
	cm2 := EnvoyConfigMap(cfg2)
	dep2 := EnvoyDeployment(cfg2)

	if cm.Data["envoy.yaml"] == cm2.Data["envoy.yaml"] {
		t.Skip("test configs produced identical ConfigMap content — adjust testCfg()")
	}

	hash2 := dep2.Spec.Template.Annotations[hashAnnotation]
	if hash == hash2 {
		t.Errorf("hash did not change when ConfigMap content changed: both = %q", hash)
	}
}

// TestEnvoyDeploymentMountsKeycloakCACert verifies that when
// auth.keycloak.tls.caCertSecretName is set, the Envoy Deployment mounts
// that Secret as an additional CA source so Envoy can verify the Keycloak
// Route certificate (router CA, not the service CA).
func TestEnvoyDeploymentMountsKeycloakCACert(t *testing.T) {
	cfg := testCfg()
	cfg.Spec.Auth.Keycloak.TLS.CACertSecretName = "my-router-ca"

	dep := EnvoyDeployment(cfg)

	// The secret must appear as a volume.
	var found bool
	for _, v := range dep.Spec.Template.Spec.Volumes {
		if v.Secret != nil && v.Secret.SecretName == "my-router-ca" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("EnvoyDeployment missing volume for keycloak caCertSecretName=%q — "+
			"Envoy will fail to verify Keycloak Route certificates", "my-router-ca")
	}
}

// TestEnvoyYAMLRejectsInjectedAudience verifies that audience values containing
// embedded newlines cannot inject YAML structure into Envoy's JWT filter config.
// Without escaping, a newline breaks out of the audience list and the injected
// content becomes new YAML keys — which could override the remote_jwks endpoint
// and route token validation to an attacker-controlled server.
//
// Best practice is structural YAML generation (not string templates); this test
// guards the current escape-at-interpolation approach as defense-in-depth.
func TestEnvoyYAMLRejectsInjectedAudience(t *testing.T) {
	cfg := testCfg()
	// Payload: embedded newline followed by a "remote_jwks:" key that would
	// override the JWKS endpoint if injected as a bare YAML line.
	cfg.Spec.Auth.Keycloak.Audiences = []string{
		"legit-audience",
		"evil\nremote_jwks:\n  http_uri:\n    uri: http://attacker.example.com/jwks",
	}

	out := EnvoyYAML(cfg)

	// After the fix the injected string is quoted as a YAML scalar — the
	// newline is escaped as \n so "remote_jwks:" never appears as a bare key.
	// The URL may appear inside a quoted scalar value, which is safe.
	if strings.Contains(out, "\nremote_jwks:") {
		t.Error("audience injection succeeded: bare 'remote_jwks:' key injected into Envoy YAML")
	}
}

// TestEnvoyYAMLRejectsInjectedIssuer verifies the issuer URL is YAML-escaped.
// A newline in issuerURL injects structure into the JWT provider config block.
func TestEnvoyYAMLRejectsInjectedIssuer(t *testing.T) {
	cfg := testCfg()
	cfg.Spec.Auth.Keycloak.IssuerURL = "https://legit.example.com\nremote_jwks:\n  http_uri:\n    uri: http://attacker.example.com/jwks"

	out := EnvoyYAML(cfg)

	if strings.Contains(out, "\nremote_jwks:") {
		t.Error("issuer injection succeeded: bare 'remote_jwks:' key injected into Envoy YAML")
	}
}
