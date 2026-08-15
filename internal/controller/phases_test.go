package controller

import (
	"errors"
	"fmt"
	"testing"
	"time"

	costv1alpha1 "github.com/project-koku/koku-service-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRunPhases_AllSucceed(t *testing.T) {
	var order []int
	phases := []PhaseFn{
		func() (Result, error) { order = append(order, 1); return Result{}, nil },
		func() (Result, error) { order = append(order, 2); return Result{}, nil },
		func() (Result, error) { order = append(order, 3); return Result{}, nil },
	}
	result, err := runPhases(phases)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsZero() {
		t.Fatalf("expected zero result, got %+v", result)
	}
	if len(order) != 3 || order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Fatalf("phases ran out of order: %v", order)
	}
}

func TestRunPhases_ErrorStopsExecution(t *testing.T) {
	sentinel := errors.New("phase 2 failed")
	var order []int
	phases := []PhaseFn{
		func() (Result, error) { order = append(order, 1); return Result{}, nil },
		func() (Result, error) { order = append(order, 2); return Result{}, sentinel },
		func() (Result, error) { order = append(order, 3); return Result{}, nil },
	}
	_, err := runPhases(phases)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
	if len(order) != 2 {
		t.Fatalf("phase 3 should not have run, order: %v", order)
	}
}

func TestRunPhases_RequeueAfterStopsExecution(t *testing.T) {
	var order []int
	phases := []PhaseFn{
		func() (Result, error) { order = append(order, 1); return Result{}, nil },
		func() (Result, error) {
			order = append(order, 2)
			return Result{RequeueAfter: 30 * time.Second}, nil
		},
		func() (Result, error) { order = append(order, 3); return Result{}, nil },
	}
	result, err := runPhases(phases)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter != 30*time.Second {
		t.Fatalf("expected 30s requeue, got %v", result.RequeueAfter)
	}
	if len(order) != 2 {
		t.Fatalf("phase 3 should not have run, order: %v", order)
	}
}

func TestRunPhases_StopHaltsWithoutRequeue(t *testing.T) {
	var order []int
	phases := []PhaseFn{
		func() (Result, error) {
			order = append(order, 1)
			return Result{Stop: true}, nil
		},
		func() (Result, error) { order = append(order, 2); return Result{}, nil },
	}
	result, err := runPhases(phases)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter != 0 || !result.Stop {
		t.Fatalf("expected Stop result, got %+v", result)
	}
	if len(order) != 1 {
		t.Fatalf("phase 2 should not have run, order: %v", order)
	}
}

func TestRunPhases_Empty(t *testing.T) {
	result, err := runPhases(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsZero() {
		t.Fatalf("expected zero result, got %+v", result)
	}
}

func TestResult_IsZero(t *testing.T) {
	tests := []struct {
		name string
		r    Result
		want bool
	}{
		{"zero", Result{}, true},
		{"requeue", Result{RequeueAfter: time.Second}, false},
		{"stop", Result{Stop: true}, false},
		{"both", Result{RequeueAfter: time.Second, Stop: true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.r.IsZero(); got != tt.want {
				t.Errorf("IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPhaseError(t *testing.T) {
	inner := errors.New("connection refused")
	pe := NewPhaseError(inner, "DatabaseReady", "ProbeFailure", costv1alpha1.PhaseDegraded)

	if pe == nil {
		t.Fatal("expected non-nil error")
	}
	if !errors.Is(pe, inner) {
		t.Error("PhaseError should unwrap to inner error")
	}

	var target *PhaseError
	if !errors.As(pe, &target) {
		t.Fatal("errors.As should match *PhaseError")
	}
	if target.ConditionType != "DatabaseReady" {
		t.Errorf("ConditionType = %q, want DatabaseReady", target.ConditionType)
	}
	if target.Reason != "ProbeFailure" {
		t.Errorf("Reason = %q, want ProbeFailure", target.Reason)
	}
	if target.Phase != costv1alpha1.PhaseDegraded {
		t.Errorf("Phase = %q, want Degraded", target.Phase)
	}

	want := "[DatabaseReady/ProbeFailure] connection refused"
	if pe.Error() != want {
		t.Errorf("Error() = %q, want %q", pe.Error(), want)
	}
}

func TestNewPhaseError_NilReturnsNil(t *testing.T) {
	if got := NewPhaseError(nil, "X", "Y", costv1alpha1.PhaseDegraded); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestApplyPhaseError(t *testing.T) {
	cfg := &costv1alpha1.CostManagementServiceConfig{
		ObjectMeta: metav1.ObjectMeta{Generation: 3},
	}
	inner := fmt.Errorf("db down")
	pe := NewPhaseError(inner, "DatabaseReady", "Unreachable", costv1alpha1.PhaseDegraded)

	applyPhaseError(cfg, pe)

	if cfg.Status.Phase != costv1alpha1.PhaseDegraded {
		t.Errorf("Phase = %q, want Degraded", cfg.Status.Phase)
	}
	found := false
	for _, c := range cfg.Status.Conditions {
		if c.Type == "DatabaseReady" {
			found = true
			if c.Status != metav1.ConditionFalse {
				t.Errorf("condition status = %q, want False", c.Status)
			}
			if c.Reason != "Unreachable" {
				t.Errorf("condition reason = %q, want Unreachable", c.Reason)
			}
			if c.ObservedGeneration != 3 {
				t.Errorf("ObservedGeneration = %d, want 3", c.ObservedGeneration)
			}
		}
	}
	if !found {
		t.Error("DatabaseReady condition not set")
	}
}

func TestApplyPhaseError_PlainErrorNoOps(t *testing.T) {
	cfg := &costv1alpha1.CostManagementServiceConfig{}
	applyPhaseError(cfg, fmt.Errorf("plain error"))
	if len(cfg.Status.Conditions) != 0 {
		t.Errorf("plain error should not set conditions, got %d", len(cfg.Status.Conditions))
	}
}
