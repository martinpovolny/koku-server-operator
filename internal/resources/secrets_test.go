package resources

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	costv1alpha1 "github.com/project-koku/koku-service-operator/api/v1alpha1"
)

func TestStorageCredentialsSecretKeys(t *testing.T) {
	cfg := &costv1alpha1.CostManagementServiceConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cost-management", Namespace: "cost-onprem"},
	}

	sec := StorageCredentialsSecret(cfg)
	if sec.Name != NameStorageSecret(cfg) {
		t.Errorf("secret name: got %q, want %q", sec.Name, NameStorageSecret(cfg))
	}
	if sec.Namespace != cfg.Namespace {
		t.Errorf("namespace: got %q, want %q", sec.Namespace, cfg.Namespace)
	}
	// KokuCommonEnv and IngressDeployment SecretKeyRefs depend on these exact keys.
	for _, key := range []string{"access-key", "secret-key"} {
		if _, ok := sec.StringData[key]; !ok {
			t.Errorf("StorageCredentialsSecret missing key %q required by S3 env wiring", key)
		}
	}
}

func TestNameStorageSecretUsesUserProvided(t *testing.T) {
	cfg := &costv1alpha1.CostManagementServiceConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cost-management"},
		Spec: costv1alpha1.CostManagementServiceConfigSpec{
			ObjectStorage: costv1alpha1.ObjectStorageConfig{
				SecretName: "my-s3-creds",
			},
		},
	}
	if got := NameStorageSecret(cfg); got != "my-s3-creds" {
		t.Fatalf("NameStorageSecret = %q, want my-s3-creds", got)
	}
}

func TestDjangoSecretKey_LengthAndCharset(t *testing.T) {
	key := djangoSecretKey(50)
	if len(key) != 50 {
		t.Fatalf("djangoSecretKey length = %d, want 50", len(key))
	}
	for i, c := range key {
		if !strings.ContainsRune(djangoKeyCharset, c) {
			t.Fatalf("djangoSecretKey[%d] = %q not in charset %q", i, c, djangoKeyCharset)
		}
	}
}

func TestDjangoSecretKey_Unique(t *testing.T) {
	seen := map[string]bool{}
	for i := range 20 {
		key := djangoSecretKey(50)
		if seen[key] {
			t.Fatalf("djangoSecretKey collision on iteration %d", i)
		}
		seen[key] = true
	}
}

func TestDjangoSecret_HoldsGeneratedKey(t *testing.T) {
	cfg := testCfg()
	sec := DjangoSecret(cfg)
	if sec.Name != NameDjangoSecret(cfg) {
		t.Errorf("name = %q, want %q", sec.Name, NameDjangoSecret(cfg))
	}
	if sec.Namespace != cfg.Namespace {
		t.Errorf("namespace = %q", sec.Namespace)
	}
	key, ok := sec.StringData["secret-key"]
	if !ok {
		t.Fatal("DjangoSecret missing secret-key")
	}
	if len(key) != 50 {
		t.Errorf("secret-key length = %d, want 50", len(key))
	}
}
