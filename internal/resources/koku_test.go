package resources

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestKokuAPIService(t *testing.T) {
	cfg := testCfg()
	svc := KokuAPIService(cfg)
	if svc.Name != NameKokuAPI(cfg) {
		t.Errorf("Name = %q, want %q", svc.Name, NameKokuAPI(cfg))
	}
	if svc.Namespace != cfg.Namespace {
		t.Errorf("Namespace = %q", svc.Namespace)
	}
	if svc.Spec.Selector[labelComponent] != "cost-management-api" {
		t.Errorf("selector component = %q", svc.Spec.Selector[labelComponent])
	}
	if len(svc.Spec.Ports) != 1 {
		t.Fatalf("ports = %+v", svc.Spec.Ports)
	}
	port := svc.Spec.Ports[0]
	if port.Name != "http" || port.Port != 8000 || port.Protocol != corev1.ProtocolTCP {
		t.Errorf("port = %+v, want http/8000/TCP", port)
	}
}

func TestMasuService(t *testing.T) {
	cfg := testCfg()
	svc := MasuService(cfg)
	if svc.Name != NameMasu(cfg) {
		t.Errorf("Name = %q, want %q", svc.Name, NameMasu(cfg))
	}
	if svc.Namespace != cfg.Namespace {
		t.Errorf("Namespace = %q", svc.Namespace)
	}
	if svc.Spec.Selector[labelComponent] != "cost-processor" {
		t.Errorf("selector component = %q", svc.Spec.Selector[labelComponent])
	}
	if len(svc.Spec.Ports) != 1 {
		t.Fatalf("ports = %+v", svc.Spec.Ports)
	}
	port := svc.Spec.Ports[0]
	if port.Name != "http" || port.Port != 9000 || port.Protocol != corev1.ProtocolTCP {
		t.Errorf("port = %+v, want http/9000/TCP", port)
	}
}
