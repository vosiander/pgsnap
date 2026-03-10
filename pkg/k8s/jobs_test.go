package k8s

import (
	"testing"

	"github.com/vosiander/pgsnap/pkg/postgres"
)

func TestBuildSQLJobResources(t *testing.T) {
	t.Parallel()

	dbConfig := &postgres.DBConfig{
		Host:     "postgres.default.svc",
		Port:     5432,
		Database: "appdb",
		User:     "appuser",
		Password: "secret",
	}

	configMap, job := buildSQLJobResources(
		"default",
		dbConfig,
		[]byte("SELECT 1;"),
		"postgres:16-alpine",
		"pgsnap-sql-123",
		"pgsnap-sql-data-123",
	)

	if configMap.Name != "pgsnap-sql-data-123" {
		t.Fatalf("unexpected ConfigMap name: %s", configMap.Name)
	}

	if string(configMap.BinaryData["query.sql"]) != "SELECT 1;" {
		t.Fatalf("unexpected SQL payload: %q", string(configMap.BinaryData["query.sql"]))
	}

	if job.Name != "pgsnap-sql-123" {
		t.Fatalf("unexpected Job name: %s", job.Name)
	}

	container := job.Spec.Template.Spec.Containers[0]
	if container.Name != "pg-sql" {
		t.Fatalf("unexpected container name: %s", container.Name)
	}

	if container.Image != "postgres:16-alpine" {
		t.Fatalf("unexpected container image: %s", container.Image)
	}

	if len(container.Command) != 1 || container.Command[0] != "psql" {
		t.Fatalf("unexpected command: %#v", container.Command)
	}

	expectedArgs := []string{
		"--host=postgres.default.svc",
		"--port=5432",
		"--username=appuser",
		"--dbname=appdb",
		"-v",
		"ON_ERROR_STOP=1",
		"--file=/sql/query.sql",
	}

	if len(container.Args) != len(expectedArgs) {
		t.Fatalf("unexpected arg count: %#v", container.Args)
	}

	for i, arg := range expectedArgs {
		if container.Args[i] != arg {
			t.Fatalf("unexpected arg at %d: got %q want %q", i, container.Args[i], arg)
		}
	}

	if len(container.Env) != 1 || container.Env[0].Name != "PGPASSWORD" || container.Env[0].Value != "secret" {
		t.Fatalf("unexpected env vars: %#v", container.Env)
	}

	if len(container.VolumeMounts) != 1 || container.VolumeMounts[0].MountPath != "/sql" {
		t.Fatalf("unexpected volume mounts: %#v", container.VolumeMounts)
	}

	if len(job.Spec.Template.Spec.Volumes) != 1 || job.Spec.Template.Spec.Volumes[0].ConfigMap == nil {
		t.Fatalf("unexpected volumes: %#v", job.Spec.Template.Spec.Volumes)
	}

	if job.Spec.Template.Spec.Volumes[0].ConfigMap.Name != "pgsnap-sql-data-123" {
		t.Fatalf("unexpected ConfigMap reference: %s", job.Spec.Template.Spec.Volumes[0].ConfigMap.Name)
	}
}
