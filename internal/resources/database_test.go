package resources

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestDatabaseService_DefaultPort(t *testing.T) {
	cfg := testCfg()
	svc := DatabaseService(cfg)
	if svc.Name != NameDatabase(cfg) {
		t.Errorf("Name = %q", svc.Name)
	}
	if svc.Spec.ClusterIP != "None" {
		t.Errorf("ClusterIP = %q, want None (headless)", svc.Spec.ClusterIP)
	}
	if svc.Spec.Selector[labelComponent] != "database" {
		t.Errorf("selector component = %q", svc.Spec.Selector[labelComponent])
	}
	if len(svc.Spec.Ports) != 1 {
		t.Fatalf("ports = %+v", svc.Spec.Ports)
	}
	port := svc.Spec.Ports[0]
	if port.Name != "postgres" || port.Port != 5432 || port.Protocol != corev1.ProtocolTCP {
		t.Errorf("port = %+v, want postgres/5432/TCP", port)
	}
}

func TestDatabaseService_CustomPort(t *testing.T) {
	cfg := testCfg()
	cfg.Spec.Database.Port = 5433
	svc := DatabaseService(cfg)
	if len(svc.Spec.Ports) != 1 || svc.Spec.Ports[0].Port != 5433 {
		t.Errorf("ports = %+v, want 5433", svc.Spec.Ports)
	}
}
