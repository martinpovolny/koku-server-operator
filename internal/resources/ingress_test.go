package resources

import (
	"testing"

	costv1alpha1 "github.com/project-koku/koku-service-operator/api/v1alpha1"
)

func TestIngressImageDefaults(t *testing.T) {
	if got := ingressImage(costv1alpha1.IngressConfig{}); got != "quay.io/iop/ingress:master" {
		t.Fatalf("empty IngressConfig image = %q", got)
	}
	cfg := costv1alpha1.IngressConfig{}
	cfg.Image.Repository = "quay.io/example/ingress"
	if got := ingressImage(cfg); got != "quay.io/example/ingress:master" {
		t.Fatalf("repo-only image = %q", got)
	}
	cfg.Image.Tag = "v1"
	if got := ingressImage(cfg); got != "quay.io/example/ingress:v1" {
		t.Fatalf("full image = %q", got)
	}
}

func TestIngressDeploymentUsesDefaultImage(t *testing.T) {
	cfg := testCfg()
	dep := IngressDeployment(cfg)
	if len(dep.Spec.Template.Spec.Containers) == 0 {
		t.Fatal("no containers")
	}
	got := dep.Spec.Template.Spec.Containers[0].Image
	if got != "quay.io/iop/ingress:master" {
		t.Fatalf("IngressDeployment image = %q, want default", got)
	}
}
