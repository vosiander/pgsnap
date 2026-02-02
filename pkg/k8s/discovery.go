package k8s

import (
	"context"
	"fmt"
	"strings"

	"github.com/vosiander/pgsnap/pkg/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Discovery handles pod discovery in Kubernetes
type Discovery struct {
	client        *kubernetes.Clientset
	namespace     string
	appIdentifier string
	podName       string // Optional: exact pod name
}

// NewDiscovery creates a new Discovery instance
func NewDiscovery(client *kubernetes.Clientset, namespace, appIdentifier, podName string) *Discovery {
	return &Discovery{
		client:        client,
		namespace:     namespace,
		appIdentifier: appIdentifier,
		podName:       podName,
	}
}

// FindPod discovers a pod using multiple strategies
func (d *Discovery) FindPod(ctx context.Context) (*corev1.Pod, error) {
	// If exact pod name is specified, use it
	if d.podName != "" {
		return d.getPodByName(ctx, d.podName)
	}

	// Try label selectors first
	pod, err := d.findByLabels(ctx)
	if err == nil && pod != nil {
		return pod, nil
	}

	// Try name pattern matching
	pod, err = d.findByNamePattern(ctx)
	if err == nil && pod != nil {
		return pod, nil
	}

	return nil, common.ErrPodNotFound
}

// getPodByName gets a pod by exact name
func (d *Discovery) getPodByName(ctx context.Context, name string) (*corev1.Pod, error) {
	pod, err := d.client.CoreV1().Pods(d.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("%w: %s", common.ErrPodNotFound, name)
	}

	if !isPodReady(pod) {
		return nil, fmt.Errorf("pod %s is not ready", name)
	}

	return pod, nil
}

// findByLabels tries to find pod using common label selectors
func (d *Discovery) findByLabels(ctx context.Context) (*corev1.Pod, error) {
	labelSelectors := []string{
		fmt.Sprintf("app.kubernetes.io/name=%s", d.appIdentifier),
		fmt.Sprintf("app=%s", d.appIdentifier),
		fmt.Sprintf("app.kubernetes.io/instance=%s", d.appIdentifier),
	}

	for _, selector := range labelSelectors {
		pods, err := d.client.CoreV1().Pods(d.namespace).List(ctx, metav1.ListOptions{
			LabelSelector: selector,
		})
		if err != nil {
			continue
		}

		readyPods := filterReadyPods(pods.Items)
		if len(readyPods) > 0 {
			return &readyPods[0], nil
		}
	}

	return nil, common.ErrPodNotFound
}

// findByNamePattern tries to find pod by name pattern
func (d *Discovery) findByNamePattern(ctx context.Context) (*corev1.Pod, error) {
	pods, err := d.client.CoreV1().Pods(d.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	readyPods := filterReadyPods(pods.Items)

	// Find pods with matching name pattern
	var matchingPods []corev1.Pod
	for _, pod := range readyPods {
		if strings.Contains(pod.Name, d.appIdentifier) {
			matchingPods = append(matchingPods, pod)
		}
	}

	if len(matchingPods) == 0 {
		return nil, common.ErrPodNotFound
	}

	if len(matchingPods) > 1 {
		return nil, common.ErrMultiplePodsFound
	}

	return &matchingPods[0], nil
}

// filterReadyPods filters pods that are ready
func filterReadyPods(pods []corev1.Pod) []corev1.Pod {
	var ready []corev1.Pod
	for _, pod := range pods {
		if isPodReady(&pod) {
			ready = append(ready, pod)
		}
	}
	return ready
}

// isPodReady checks if a pod is ready
func isPodReady(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}

	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}

	return false
}
