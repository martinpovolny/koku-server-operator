package resources

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	costv1alpha1 "github.com/project-koku/koku-service-operator/api/v1alpha1"
)

func kruizeCfg() *costv1alpha1.CostManagementServiceConfig {
	cfg := rosCfg()
	cfg.Spec.Kruize.Image.Repository = "quay.io/kruize/autotune_operator"
	cfg.Spec.Kruize.Image.Tag = "latest"
	return cfg
}

func TestKruizeServiceAccount_NameAndLabels(t *testing.T) {
	cfg := kruizeCfg()
	sa := KruizeServiceAccount(cfg)
	if sa.Name != NameKruizeServiceAccount(cfg) {
		t.Errorf("Name = %q, want %q", sa.Name, NameKruizeServiceAccount(cfg))
	}
	if sa.Namespace != cfg.Namespace {
		t.Errorf("Namespace = %q", sa.Namespace)
	}
	if sa.Labels[labelComponent] != "ros-optimization" {
		t.Errorf("component label = %q", sa.Labels[labelComponent])
	}
}

func TestKruizeConfigMap_ContainsDBAndKafka(t *testing.T) {
	cfg := kruizeCfg()
	cm := KruizeConfigMap(cfg)
	if cm.Name != NameKruizeConfigMap(cfg) {
		t.Errorf("Name = %q", cm.Name)
	}
	data := cm.Data["cdappconfig.json"]
	for _, want := range []string{
		`"hostname": "postgres.example.svc"`,
		`"name": "` + kruizeDBName + `"`,
		`"hostname": "kafka.example.com"`,
	} {
		if !strings.Contains(data, want) {
			t.Errorf("cdappconfig missing %q\n%s", want, data)
		}
	}
}

func TestKruizeDeployment_Shape(t *testing.T) {
	cfg := kruizeCfg()
	cfg.Spec.Kruize.Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
	}
	d := KruizeDeployment(cfg)
	if d.Name != NameKruize(cfg) {
		t.Errorf("Name = %q, want %q", d.Name, NameKruize(cfg))
	}
	if d.Spec.Template.Spec.ServiceAccountName != NameKruizeServiceAccount(cfg) {
		t.Errorf("SA = %q", d.Spec.Template.Spec.ServiceAccountName)
	}
	if len(d.Spec.Template.Spec.InitContainers) < 1 {
		t.Fatal("expected wait-for-db init container")
	}
	c := d.Spec.Template.Spec.Containers[0]
	if c.Name != "kruize" {
		t.Errorf("container name = %q", c.Name)
	}
	if len(c.Ports) != 1 || c.Ports[0].ContainerPort != kruizePort {
		t.Errorf("ports = %+v, want %d", c.Ports, kruizePort)
	}
	if c.LivenessProbe == nil || c.LivenessProbe.HTTPGet == nil || c.LivenessProbe.HTTPGet.Path != "/listPerformanceProfiles" {
		t.Errorf("liveness probe = %+v", c.LivenessProbe)
	}
	if c.ReadinessProbe == nil || c.ReadinessProbe.HTTPGet == nil || c.ReadinessProbe.HTTPGet.Path != "/listPerformanceProfiles" {
		t.Errorf("readiness probe = %+v", c.ReadinessProbe)
	}
	if c.Resources.Requests.Memory().String() != "1Gi" {
		t.Errorf("memory request = %s, want 1Gi from spec.kruize.resources", c.Resources.Requests.Memory())
	}
}

func TestKruizeService_PortAndSelector(t *testing.T) {
	cfg := kruizeCfg()
	svc := KruizeService(cfg)
	if svc.Name != NameKruize(cfg) {
		t.Errorf("Name = %q", svc.Name)
	}
	if len(svc.Spec.Ports) != 1 || svc.Spec.Ports[0].Port != kruizePort {
		t.Errorf("ports = %+v, want %d", svc.Spec.Ports, kruizePort)
	}
	if svc.Spec.Selector[labelComponent] != "ros-optimization" {
		t.Errorf("selector component = %q", svc.Spec.Selector[labelComponent])
	}
}
