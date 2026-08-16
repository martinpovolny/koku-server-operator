package resources

import (
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// serviceMonitorDoc is the subset of ServiceMonitor YAML the operator emits.
// Tests round-trip through yaml.Unmarshal so selector values are real strings,
// not a silent type confusion from unstructured maps.
type serviceMonitorDoc struct {
	Spec struct {
		Selector struct {
			MatchExpressions []struct {
				Key      string   `yaml:"key"`
				Operator string   `yaml:"operator"`
				Values   []string `yaml:"values"`
			} `yaml:"matchExpressions"`
		} `yaml:"selector"`
	} `yaml:"spec"`
}

func serviceMonitorComponents(t *testing.T, sm *unstructured.Unstructured) []string {
	t.Helper()
	raw, err := yaml.Marshal(sm.Object)
	if err != nil {
		t.Fatalf("marshal ServiceMonitor: %v", err)
	}
	var doc serviceMonitorDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("ServiceMonitor YAML round-trip: %v\n%s", err, raw)
	}
	for _, expr := range doc.Spec.Selector.MatchExpressions {
		if expr.Key == "app.kubernetes.io/component" {
			return expr.Values
		}
	}
	t.Fatalf("no app.kubernetes.io/component matchExpression in:\n%s", raw)
	return nil
}

func TestAppServiceMonitor_OmitsROSWhenDisabled(t *testing.T) {
	cfg := testCfg() // ROS.Enabled nil → false
	comps := serviceMonitorComponents(t, AppServiceMonitor(cfg))
	if slices.Contains(comps, "ros-api") {
		t.Errorf("AppServiceMonitor selected ros-api with ros.enabled=false: %v", comps)
	}
	for _, want := range []string{"cost-management-api", "cost-processor", "listener", "ingress"} {
		if !slices.Contains(comps, want) {
			t.Errorf("AppServiceMonitor missing %q: %v", want, comps)
		}
	}
}

func TestAppServiceMonitor_IncludesROSWhenEnabled(t *testing.T) {
	cfg := testCfg()
	enabled := true
	cfg.Spec.ROS.Enabled = &enabled
	comps := serviceMonitorComponents(t, AppServiceMonitor(cfg))
	if !slices.Contains(comps, "ros-api") {
		t.Errorf("AppServiceMonitor missing ros-api with ros.enabled=true: %v", comps)
	}
}

func TestKruizeServiceMonitor_SelectsKruize(t *testing.T) {
	cfg := testCfg()
	sm := KruizeServiceMonitor(cfg)
	if sm.GetName() != cfg.Name+"-kruize-metrics" {
		t.Errorf("name = %q", sm.GetName())
	}
	comps := serviceMonitorComponents(t, sm)
	if !slices.Contains(comps, "ros-optimization") {
		t.Errorf("KruizeServiceMonitor missing ros-optimization: %v", comps)
	}
}
