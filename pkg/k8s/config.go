package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ExtractEnvVars extracts environment variables from a pod, including values from Secrets and ConfigMaps
func ExtractEnvVars(ctx context.Context, client *kubernetes.Clientset, pod *corev1.Pod) (map[string]string, error) {
	envVars := make(map[string]string)

	// Get the first container (assuming app is in first container)
	if len(pod.Spec.Containers) == 0 {
		return envVars, nil
	}

	container := pod.Spec.Containers[0]

	// Extract env vars (direct values and from Secrets/ConfigMaps)
	for _, env := range container.Env {
		// Direct value
		if env.Value != "" {
			envVars[env.Name] = env.Value
			continue
		}

		// Value from Secret
		if env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil {
			secretRef := env.ValueFrom.SecretKeyRef
			secret, err := client.CoreV1().Secrets(pod.Namespace).Get(ctx, secretRef.Name, metav1.GetOptions{})
			if err != nil {
				return nil, fmt.Errorf("failed to get secret %s: %w", secretRef.Name, err)
			}
			if value, ok := secret.Data[secretRef.Key]; ok {
				envVars[env.Name] = string(value)
			}
			continue
		}

		// Value from ConfigMap
		if env.ValueFrom != nil && env.ValueFrom.ConfigMapKeyRef != nil {
			cmRef := env.ValueFrom.ConfigMapKeyRef
			configMap, err := client.CoreV1().ConfigMaps(pod.Namespace).Get(ctx, cmRef.Name, metav1.GetOptions{})
			if err != nil {
				return nil, fmt.Errorf("failed to get configmap %s: %w", cmRef.Name, err)
			}
			if value, ok := configMap.Data[cmRef.Key]; ok {
				envVars[env.Name] = value
			}
			continue
		}
	}

	return envVars, nil
}
