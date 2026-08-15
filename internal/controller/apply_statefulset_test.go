package controller

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	costv1alpha1 "github.com/project-koku/koku-service-operator/api/v1alpha1"
	"github.com/project-koku/koku-service-operator/internal/resources"
)

func TestReconcileInfrastructure_ApplyStatefulSetCreate(t *testing.T) {
	scheme := ownershipScheme(t)
	cfg := minimalCR(testCRName, testNamespace)
	cfg.Spec.Cache.Deploy = boolPtr(false)
	// Database.Deploy defaults true → applyStatefulSet create path.

	r := &CostManagementServiceConfigReconciler{
		Client:   fakeClientWithApplySupport(scheme),
		Scheme:   scheme,
		Recorder: &noopRecorder{},
	}

	result, err := r.reconcileInfrastructure(context.Background(), cfg)
	if err != nil {
		t.Fatalf("reconcileInfrastructure: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Fatal("expected requeue while StatefulSet is not ready")
	}

	mustExist(t, r.Client, testNamespace, resources.NameDatabase(cfg), &corev1.Service{})
	sts := &appsv1.StatefulSet{}
	mustExist(t, r.Client, testNamespace, resources.NameDatabase(cfg), sts)

	cond := findCondition(cfg.Status.Conditions, costv1alpha1.ConditionDatabaseReady)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != "WaitingForDatabase" {
		t.Fatalf("expected DatabaseReady=False WaitingForDatabase, got %+v", cond)
	}
}

func TestApplyStatefulSet_UpdatePreservesVolumeClaimTemplates(t *testing.T) {
	scheme := ownershipScheme(t)
	cfg := minimalCR(testCRName, testNamespace)

	existing := resources.DatabaseStatefulSet(cfg)
	existing.Spec.Template.Spec.Containers[0].Image = "postgres:old"
	existing.Spec.VolumeClaimTemplates[0].Spec.Resources.Requests[corev1.ResourceStorage] = resource.MustParse("10Gi")
	replicas := int32(1)
	existing.Spec.Replicas = &replicas

	r := &CostManagementServiceConfigReconciler{
		Client:   fakeClientWithApplySupport(scheme, existing),
		Scheme:   scheme,
		Recorder: &noopRecorder{},
	}

	desired := resources.DatabaseStatefulSet(cfg)
	desired.Spec.Template.Spec.Containers[0].Image = "postgres:new"
	wantReplicas := int32(2)
	desired.Spec.Replicas = &wantReplicas
	// Attempt to change VCT size — applyStatefulSet must ignore this.
	desired.Spec.VolumeClaimTemplates[0].Spec.Resources.Requests[corev1.ResourceStorage] = resource.MustParse("99Gi")

	if err := r.applyStatefulSet(context.Background(), cfg, desired); err != nil {
		t.Fatalf("applyStatefulSet: %v", err)
	}

	got := &appsv1.StatefulSet{}
	if err := r.Get(context.Background(), types.NamespacedName{
		Namespace: testNamespace,
		Name:      resources.NameDatabase(cfg),
	}, got); err != nil {
		t.Fatalf("Get StatefulSet: %v", err)
	}

	if got.Spec.Template.Spec.Containers[0].Image != "postgres:new" {
		t.Errorf("image not updated: got %q", got.Spec.Template.Spec.Containers[0].Image)
	}
	if got.Spec.Replicas == nil || *got.Spec.Replicas != 2 {
		t.Errorf("replicas not updated: got %v", got.Spec.Replicas)
	}
	size := got.Spec.VolumeClaimTemplates[0].Spec.Resources.Requests[corev1.ResourceStorage]
	if !size.Equal(resource.MustParse("10Gi")) {
		t.Errorf("VolumeClaimTemplates must be immutable; got size %s want 10Gi", size.String())
	}
}
