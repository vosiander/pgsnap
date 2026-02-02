package k8s

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/vosiander/pgsnap/pkg/common"
	"github.com/vosiander/pgsnap/pkg/postgres"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// BackupJob represents a Kubernetes Job for database backup
type BackupJob struct {
	client    *kubernetes.Clientset
	config    *rest.Config
	namespace string
	name      string
	dbConfig  *postgres.DBConfig
}

// Name returns the job name
func (j *BackupJob) Name() string {
	return j.name
}

// CreateBackupJob creates a Kubernetes Job that runs pg_dump
func CreateBackupJob(ctx context.Context, client *kubernetes.Clientset, config *rest.Config, namespace string, dbConfig *postgres.DBConfig, postgresImage string) (*BackupJob, error) {
	timestamp := time.Now().Format("20060102-150405")
	jobName := fmt.Sprintf("pgsnap-backup-%s", timestamp)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: namespace,
			Labels: map[string]string{
				"app":       "pgsnap",
				"operation": "backup",
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: int32Ptr(0), // Don't retry on failure
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":       "pgsnap",
						"operation": "backup",
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:  "pg-dump",
							Image: postgresImage,
							Command: []string{
								"sh",
								"-c",
								fmt.Sprintf(`pg_dump \
									--format=plain \
									--no-owner \
									--no-acl \
									--clean \
									--if-exists \
									--host=%s \
									--port=%d \
									--username=%s \
									--dbname=%s \
									--file=/backup/dump.sql && \
									echo "Backup completed successfully" && \
									sleep 600`,
									dbConfig.Host,
									dbConfig.Port,
									dbConfig.User,
									dbConfig.Database),
							},
							Env: []corev1.EnvVar{
								{Name: "PGPASSWORD", Value: dbConfig.Password},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "backup", MountPath: "/backup"},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "backup",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}

	createdJob, err := client.BatchV1().Jobs(namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create backup job: %w", err)
	}

	return &BackupJob{
		client:    client,
		config:    config,
		namespace: namespace,
		name:      createdJob.Name,
		dbConfig:  dbConfig,
	}, nil
}

// WaitForCompletion waits for the backup to be ready by monitoring pod logs
func (j *BackupJob) WaitForCompletion(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for backup to complete")
		case <-ticker.C:
			// Check if Job has failed
			job, err := j.client.BatchV1().Jobs(j.namespace).Get(ctx, j.name, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("failed to get job status: %w", err)
			}

			if job.Status.Failed > 0 {
				logs, _ := j.GetPodLogs(ctx)
				return fmt.Errorf("backup job failed. Logs:\n%s", logs)
			}

			// Check logs for success message (don't wait for Job completion!)
			logs, err := j.GetPodLogs(ctx)
			if err != nil {
				// Pod might not be ready yet, continue waiting
				continue
			}

			// Look for the success message
			if len(logs) > 0 && (containsString(logs, "Backup completed successfully") || containsString(logs, "pg_dump")) {
				// Backup is ready! Return immediately while container is still alive
				return nil
			}
		}
	}
}

// GetPod returns the pod created by this Job
func (j *BackupJob) GetPod(ctx context.Context) (*corev1.Pod, error) {
	pods, err := j.client.CoreV1().Pods(j.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", j.name),
	})
	if err != nil {
		return nil, err
	}

	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("no pod found for job %s", j.name)
	}

	return &pods.Items[0], nil
}

// GetPodLogs retrieves logs from the job pod
func (j *BackupJob) GetPodLogs(ctx context.Context) (string, error) {
	pod, err := j.GetPod(ctx)
	if err != nil {
		return "", err
	}

	req := j.client.CoreV1().Pods(j.namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
		Container: "pg-dump",
	})

	logs, err := req.Stream(ctx)
	if err != nil {
		return "", err
	}
	defer logs.Close()

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, logs)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

// DownloadBackup downloads the backup file from the pod
func (j *BackupJob) DownloadBackup(ctx context.Context, localPath string) error {
	pod, err := j.GetPod(ctx)
	if err != nil {
		return err
	}

	// Ensure output directory exists
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Execute tar command in pod to stream file
	req := j.client.CoreV1().RESTClient().
		Post().
		Namespace(j.namespace).
		Resource("pods").
		Name(pod.Name).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: "pg-dump",
			Command:   []string{"tar", "cf", "-", "-C", "/backup", "dump.sql"},
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(j.config, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("failed to create executor: %w", err)
	}

	// Create local file
	outFile, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create local file: %w", err)
	}
	defer outFile.Close()

	// Stream from pod
	var stderr bytes.Buffer
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: outFile,
		Stderr: &stderr,
	})
	if err != nil {
		return fmt.Errorf("failed to download backup: %w, stderr: %s", err, stderr.String())
	}

	// Extract tar file
	if err := extractTarFile(localPath); err != nil {
		return fmt.Errorf("failed to extract backup: %w", err)
	}

	return nil
}

// Cleanup deletes the Job and associated resources
func (j *BackupJob) Cleanup(ctx context.Context) error {
	deletePolicy := metav1.DeletePropagationForeground
	return j.client.BatchV1().Jobs(j.namespace).Delete(ctx, j.name, metav1.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	})
}

// RestoreJob represents a Kubernetes Job for database restore
type RestoreJob struct {
	client    *kubernetes.Clientset
	config    *rest.Config
	namespace string
	name      string
	configMap string
	dbConfig  *postgres.DBConfig
}

// CreateRestoreJob creates a Kubernetes Job that runs psql to restore
func CreateRestoreJob(ctx context.Context, client *kubernetes.Clientset, config *rest.Config, namespace string, dbConfig *postgres.DBConfig, backupData []byte, postgresImage string) (*RestoreJob, error) {
	timestamp := time.Now().Format("20060102-150405")
	jobName := fmt.Sprintf("pgsnap-restore-%s", timestamp)
	configMapName := fmt.Sprintf("pgsnap-restore-data-%s", timestamp)

	// Create ConfigMap with backup data
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: namespace,
			Labels: map[string]string{
				"app":       "pgsnap",
				"operation": "restore",
			},
		},
		BinaryData: map[string][]byte{
			"backup.sql": backupData,
		},
	}

	_, err := client.CoreV1().ConfigMaps(namespace).Create(ctx, configMap, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create ConfigMap: %w", err)
	}

	// Create Job
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: namespace,
			Labels: map[string]string{
				"app":       "pgsnap",
				"operation": "restore",
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: int32Ptr(0),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":       "pgsnap",
						"operation": "restore",
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:  "pg-restore",
							Image: postgresImage,
							Command: []string{
								"sh",
								"-c",
								fmt.Sprintf(`psql \
									--host=%s \
									--port=%d \
									--username=%s \
									--dbname=%s \
									--file=/backup/backup.sql && \
									echo "Restore completed successfully"`,
									dbConfig.Host,
									dbConfig.Port,
									dbConfig.User,
									dbConfig.Database),
							},
							Env: []corev1.EnvVar{
								{Name: "PGPASSWORD", Value: dbConfig.Password},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "backup", MountPath: "/backup"},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "backup",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: configMapName,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	createdJob, err := client.BatchV1().Jobs(namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		// Clean up ConfigMap if job creation fails
		client.CoreV1().ConfigMaps(namespace).Delete(ctx, configMapName, metav1.DeleteOptions{})
		return nil, fmt.Errorf("failed to create restore job: %w", err)
	}

	return &RestoreJob{
		client:    client,
		config:    config,
		namespace: namespace,
		name:      createdJob.Name,
		configMap: configMapName,
		dbConfig:  dbConfig,
	}, nil
}

// WaitForCompletion waits for the restore Job to complete
func (r *RestoreJob) WaitForCompletion(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for restore job to complete")
		case <-ticker.C:
			job, err := r.client.BatchV1().Jobs(r.namespace).Get(ctx, r.name, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("failed to get job status: %w", err)
			}

			if job.Status.Succeeded > 0 {
				return nil
			}

			if job.Status.Failed > 0 {
				logs, _ := r.GetPodLogs(ctx)
				return fmt.Errorf("%w. Logs:\n%s", common.ErrRestoreFailed, logs)
			}
		}
	}
}

// GetPod returns the pod created by this Job
func (r *RestoreJob) GetPod(ctx context.Context) (*corev1.Pod, error) {
	pods, err := r.client.CoreV1().Pods(r.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", r.name),
	})
	if err != nil {
		return nil, err
	}

	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("no pod found for job %s", r.name)
	}

	return &pods.Items[0], nil
}

// GetPodLogs retrieves logs from the restore job pod
func (r *RestoreJob) GetPodLogs(ctx context.Context) (string, error) {
	pod, err := r.GetPod(ctx)
	if err != nil {
		return "", err
	}

	req := r.client.CoreV1().Pods(r.namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
		Container: "pg-restore",
	})

	logs, err := req.Stream(ctx)
	if err != nil {
		return "", err
	}
	defer logs.Close()

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, logs)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

// Cleanup deletes the Job, ConfigMap, and associated resources
func (r *RestoreJob) Cleanup(ctx context.Context) error {
	deletePolicy := metav1.DeletePropagationForeground

	// Delete Job
	if err := r.client.BatchV1().Jobs(r.namespace).Delete(ctx, r.name, metav1.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	}); err != nil {
		return err
	}

	// Delete ConfigMap
	return r.client.CoreV1().ConfigMaps(r.namespace).Delete(ctx, r.configMap, metav1.DeleteOptions{})
}

// Helper functions

func int32Ptr(i int32) *int32 {
	return &i
}

// containsString checks if a string contains a substring
func containsString(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			(len(s) > len(substr) &&
				(hasPrefix(s, substr) ||
					hasSuffix(s, substr) ||
					hasMiddle(s, substr))))
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

func hasMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// extractTarFile extracts a single file from a tar archive
func extractTarFile(tarPath string) error {
	// Open tar file
	tarFile, err := os.Open(tarPath)
	if err != nil {
		return fmt.Errorf("failed to open tar file: %w", err)
	}
	defer tarFile.Close()

	// Create tar reader
	tarReader := tar.NewReader(tarFile)

	// Find and extract dump.sql
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar: %w", err)
		}

		if header.Name == "dump.sql" {
			// Create output file (replace .tar extension with .sql)
			sqlPath := tarPath[:len(tarPath)-4] + ".sql"
			outFile, err := os.Create(sqlPath)
			if err != nil {
				return fmt.Errorf("failed to create output file: %w", err)
			}
			defer outFile.Close()

			// Copy content
			if _, err := io.Copy(outFile, tarReader); err != nil {
				return fmt.Errorf("failed to extract file: %w", err)
			}

			// Remove tar file
			os.Remove(tarPath)
			return nil
		}
	}

	return fmt.Errorf("dump.sql not found in tar archive")
}
