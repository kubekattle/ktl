// snapshot.go powers 'ktl diag snapshot' save/replay/diff flows by orchestrating collectors and writers.
package snapshot

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/example/ktl/internal/kube"
	"github.com/pmezard/go-difflib/difflib"
	"golang.org/x/exp/maps"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	serializerjson "k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/pointer"
	goruntime "runtime"
	sigyaml "sigs.k8s.io/yaml"
)

var (
	yamlSerializer = serializerjson.NewSerializerWithOptions(serializerjson.DefaultMetaFactory, nil, nil, serializerjson.SerializerOptions{Yaml: true, Pretty: true, Strict: false})
)

type Saver struct {
	Client   *kube.Client
	LogLines int64
}

type Metadata struct {
	Namespace  string    `json:"namespace"`
	CapturedAt time.Time `json:"capturedAt"`
	Resources  int       `json:"resources"`
	Pods       int       `json:"pods"`
	LogLines   int64     `json:"logLines"`
	Kubernetes string    `json:"kubernetesVersion"`
}

func (s *Saver) Save(ctx context.Context, namespace, outputPath string) error {
	if s.LogLines <= 0 {
		s.LogLines = 200
	}

	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create snapshot output: %w", err)
	}
	defer file.Close()

	gz := gzip.NewWriter(file)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	meta := Metadata{
		Namespace:  namespace,
		CapturedAt: time.Now().UTC(),
		LogLines:   s.LogLines,
	}
	if versionInfo, err := s.Client.Clientset.Discovery().ServerVersion(); err == nil {
		meta.Kubernetes = versionInfo.String()
	}

	if err := s.captureResources(ctx, namespace, tw, &meta); err != nil {
		return err
	}
	if err := s.capturePods(ctx, namespace, tw, &meta); err != nil {
		return err
	}
	if err := s.captureEvents(ctx, namespace, tw); err != nil {
		return err
	}
	if err := s.captureMetrics(ctx, namespace, tw); err != nil {
		return err
	}

	payload, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	if err := addFile(tw, "metadata.json", payload); err != nil {
		return err
	}
	return nil
}

func (s *Saver) captureResources(ctx context.Context, namespace string, tw *tar.Writer, meta *Metadata) error {
	writers := []struct {
		prefix string
		list   func(context.Context, string) ([]runtime.Object, error)
	}{
		{"deployments", s.listDeployments},
		{"statefulsets", s.listStatefulSets},
		{"daemonsets", s.listDaemonSets},
		{"jobs", s.listJobs},
		{"services", s.listServices},
		{"ingresses", s.listIngresses},
		{"configmaps", s.listConfigMaps},
	}
	for _, w := range writers {
		objs, err := w.list(ctx, namespace)
		if err != nil {
			return err
		}
		for _, obj := range objs {
			meta.Resources++
			name := obj.(metav1.Object).GetName()
			path := fmt.Sprintf("resources/%s/%s.yaml", w.prefix, sanitizeFileName(name))
			if err := addBufferFile(tw, path, func(buf *bytes.Buffer) error {
				return yamlSerializer.Encode(obj, buf)
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Saver) capturePods(ctx context.Context, namespace string, tw *tar.Writer, meta *Metadata) error {
	pods, err := s.Client.Clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list pods: %w", err)
	}
	for _, pod := range pods.Items {
		meta.Pods++
		path := fmt.Sprintf("pods/%s.yaml", sanitizeFileName(pod.Name))
		if err := addBufferFile(tw, path, func(buf *bytes.Buffer) error {
			return yamlSerializer.Encode(&pod, buf)
		}); err != nil {
			return err
		}
		if err := s.capturePodLogs(ctx, namespace, &pod, tw); err != nil {
			return err
		}
	}
	return nil
}

func (s *Saver) capturePodLogs(ctx context.Context, namespace string, pod *corev1.Pod, tw *tar.Writer) error {
	containers := append([]corev1.Container{}, pod.Spec.InitContainers...)
	containers = append(containers, pod.Spec.Containers...)
	for _, ctr := range containers {
		req := s.Client.Clientset.CoreV1().Pods(namespace).GetLogs(pod.Name, &corev1.PodLogOptions{Container: ctr.Name, TailLines: pointer.Int64(s.LogLines)})
		stream, err := req.Stream(ctx)
		if err != nil {
			continue
		}
		data, err := io.ReadAll(stream)
		stream.Close()
		if err != nil {
			continue
		}
		if len(data) == 0 {
			continue
		}
		path := fmt.Sprintf("logs/%s/%s.log", sanitizeFileName(pod.Name), sanitizeFileName(ctr.Name))
		if err := addFile(tw, path, data); err != nil {
			return err
		}
	}
	return nil
}

func (s *Saver) captureEvents(ctx context.Context, namespace string, tw *tar.Writer) error {
	events, err := s.Client.Clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list events: %w", err)
	}
	return addBufferFile(tw, "events.yaml", func(buf *bytes.Buffer) error {
		return yamlSerializer.Encode(events, buf)
	})
}

func (s *Saver) captureMetrics(ctx context.Context, namespace string, tw *tar.Writer) error {
	metrics, err := s.Client.Metrics.MetricsV1beta1().PodMetricses(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}
	payload, err := json.MarshalIndent(metrics, "", "  ")
	if err != nil {
		return err
	}
	return addFile(tw, "metrics/pod-metrics.json", payload)
}

// list functions (deployment, etc.)
func (s *Saver) listDeployments(ctx context.Context, namespace string) ([]runtime.Object, error) {
	list, err := s.Client.Clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list deployments: %w", err)
	}
	objs := make([]runtime.Object, len(list.Items))
	for i := range list.Items {
		objs[i] = &list.Items[i]
	}
	return objs, nil
}

func (s *Saver) listStatefulSets(ctx context.Context, namespace string) ([]runtime.Object, error) {
	list, err := s.Client.Clientset.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list statefulsets: %w", err)
	}
	objs := make([]runtime.Object, len(list.Items))
	for i := range list.Items {
		objs[i] = &list.Items[i]
	}
	return objs, nil
}

func (s *Saver) listDaemonSets(ctx context.Context, namespace string) ([]runtime.Object, error) {
	list, err := s.Client.Clientset.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list daemonsets: %w", err)
	}
	objs := make([]runtime.Object, len(list.Items))
	for i := range list.Items {
		objs[i] = &list.Items[i]
	}
	return objs, nil
}

func (s *Saver) listJobs(ctx context.Context, namespace string) ([]runtime.Object, error) {
	jobs, err := s.Client.Clientset.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	objs := make([]runtime.Object, len(jobs.Items))
	for i := range jobs.Items {
		objs[i] = &jobs.Items[i]
	}
	return objs, nil
}

func (s *Saver) listServices(ctx context.Context, namespace string) ([]runtime.Object, error) {
	svcs, err := s.Client.Clientset.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	objs := make([]runtime.Object, len(svcs.Items))
	for i := range svcs.Items {
		objs[i] = &svcs.Items[i]
	}
	return objs, nil
}

func (s *Saver) listIngresses(ctx context.Context, namespace string) ([]runtime.Object, error) {
	ing, err := s.Client.Clientset.NetworkingV1().Ingresses(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list ingresses: %w", err)
	}
	objs := make([]runtime.Object, len(ing.Items))
	for i := range ing.Items {
		objs[i] = &ing.Items[i]
	}
	return objs, nil
}

func (s *Saver) listConfigMaps(ctx context.Context, namespace string) ([]runtime.Object, error) {
	cms, err := s.Client.Clientset.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list configmaps: %w", err)
	}
	objs := make([]runtime.Object, 0, len(cms.Items))
	for i := range cms.Items {
		objs = append(objs, &cms.Items[i])
	}
	return objs, nil
}

func addFile(tw *tar.Writer, name string, data []byte) error {
	hdr := &tar.Header{
		Name: name,
		Mode: 0o644,
		Size: int64(len(data)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

func addBufferFile(tw *tar.Writer, name string, fill func(*bytes.Buffer) error) error {
	var buf bytes.Buffer
	if err := fill(&buf); err != nil {
		return err
	}
	hdr := &tar.Header{
		Name: name,
		Mode: 0o644,
		Size: int64(buf.Len()),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err := buf.WriteTo(tw)
	return err
}

func sanitizeFileName(name string) string {
	name = strings.ReplaceAll(name, "..", "")
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	return name
}

type Replayer struct {
	Client *kube.Client
}

func (r *Replayer) Replay(ctx context.Context, archivePath, namespace string, createNamespace bool) error {
	if createNamespace {
		if err := ensureNamespace(ctx, r.Client, namespace); err != nil {
			return err
		}
	}
	type resourceEntry struct {
		data []byte
	}
	entries := make([]resourceEntry, 0)
	if err := iterateArchive(archivePath, func(hdr *tar.Header, data []byte) error {
		if strings.HasPrefix(hdr.Name, "resources/") {
			entries = append(entries, resourceEntry{data: append([]byte(nil), data...)})
		}
		return nil
	}); err != nil {
		return err
	}
	workerLimit := goruntime.NumCPU()
	if workerLimit < 2 {
		workerLimit = 2
	}
	sem := make(chan struct{}, workerLimit)
	var g errgroup.Group
	for _, entry := range entries {
		entry := entry
		sem <- struct{}{}
		g.Go(func() error {
			defer func() { <-sem }()
			return r.applyResource(ctx, entry.data, namespace)
		})
	}
	return g.Wait()
}

func ensureNamespace(ctx context.Context, client *kube.Client, namespace string) error {
	if namespace == "" {
		return nil
	}
	_, err := client.Clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err == nil {
		return nil
	}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
	_, err = client.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	return err
}

func (r *Replayer) applyResource(ctx context.Context, data []byte, targetNamespace string) error {
	jsonData, err := sigyaml.YAMLToJSON(data)
	if err != nil {
		return fmt.Errorf("convert manifest to json: %w", err)
	}
	var obj unstructured.Unstructured
	if err := obj.UnmarshalJSON(jsonData); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}
	mapping, err := r.Client.RESTMapper.RESTMapping(obj.GroupVersionKind().GroupKind(), obj.GroupVersionKind().Version)
	if err != nil {
		return fmt.Errorf("rest mapping for %s: %w", obj.GroupVersionKind(), err)
	}
	ns := obj.GetNamespace()
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		if targetNamespace != "" {
			ns = targetNamespace
			obj.SetNamespace(ns)
		} else if ns == "" {
			ns = r.Client.Namespace
			obj.SetNamespace(ns)
		}
	} else {
		ns = ""
		obj.SetNamespace("")
	}
	obj.SetManagedFields(nil)
	obj.SetResourceVersion("")
	cleanJSON, err := obj.MarshalJSON()
	if err != nil {
		return err
	}
	force := true
	resource := r.Client.Dynamic.Resource(mapping.Resource)
	if ns != "" && mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		_, err = resource.Namespace(ns).Patch(ctx, obj.GetName(), types.ApplyPatchType, cleanJSON, metav1.PatchOptions{FieldManager: "ktl-snapshot-replay", Force: &force})
	} else {
		_, err = resource.Patch(ctx, obj.GetName(), types.ApplyPatchType, cleanJSON, metav1.PatchOptions{FieldManager: "ktl-snapshot-replay", Force: &force})
	}
	return err
}

func DiffArchives(aPath, bPath string) (string, error) {
	aFiles, err := readArchiveSubset(aPath)
	if err != nil {
		return "", err
	}
	bFiles, err := readArchiveSubset(bPath)
	if err != nil {
		return "", err
	}
	keys := sets.NewString()
	keys.Insert(maps.Keys(aFiles)...)
	keys.Insert(maps.Keys(bFiles)...)
	list := keys.List()
	sort.Strings(list)
	var b strings.Builder
	for _, key := range list {
		a := string(aFiles[key])
		c := string(bFiles[key])
		if a == c {
			continue
		}
		ud := difflib.UnifiedDiff{
			A:        difflib.SplitLines(a),
			B:        difflib.SplitLines(c),
			FromFile: fmt.Sprintf("%s:%s", aPath, key),
			ToFile:   fmt.Sprintf("%s:%s", bPath, key),
			Context:  3,
		}
		diff, _ := difflib.GetUnifiedDiffString(ud)
		if diff == "" {
			continue
		}
		b.WriteString(diff)
		if !strings.HasSuffix(diff, "\n") {
			b.WriteString("\n")
		}
	}
	if b.Len() == 0 {
		return "no differences found\n", nil
	}
	return b.String(), nil
}

func readArchiveSubset(path string) (map[string][]byte, error) {
	files := make(map[string][]byte)
	err := iterateArchive(path, func(hdr *tar.Header, data []byte) error {
		if strings.HasPrefix(hdr.Name, "resources/") || strings.HasPrefix(hdr.Name, "pods/") {
			files[hdr.Name] = append([]byte(nil), data...)
		}
		return nil
	})
	return files, err
}

func iterateArchive(archivePath string, handle func(*tar.Header, []byte) error) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		buf := new(bytes.Buffer)
		if _, err := io.Copy(buf, tr); err != nil {
			return err
		}
		if err := handle(hdr, buf.Bytes()); err != nil {
			return err
		}
	}
	return nil
}
