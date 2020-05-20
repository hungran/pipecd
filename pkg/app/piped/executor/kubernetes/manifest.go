// Copyright 2020 The PipeCD Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kubernetes

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/kapetaniosci/pipe/pkg/config"
)

type Manifest struct {
	APIVersion string
	Kind       string
	Namespace  string
	Name       string
	u          *unstructured.Unstructured
}

func (m Manifest) Duplicate(name string) Manifest {
	u := m.u.DeepCopy()
	u.SetName(name)
	return Manifest{
		APIVersion: m.APIVersion,
		Kind:       m.Kind,
		Namespace:  m.Namespace,
		Name:       name,
		u:          u,
	}
}

func (m Manifest) YamlBytes() ([]byte, error) {
	return yaml.Marshal(m.u)
}

func (m Manifest) AddAnnotations(annotations map[string]string) {
	if len(annotations) == 0 {
		return
	}

	annos := m.u.GetAnnotations()
	if annos != nil {
		for k, v := range annotations {
			annos[k] = v
		}
	} else {
		annos = annotations
	}
	m.u.SetAnnotations(annos)
}

func (m Manifest) SetReplicas(replicas int) {
	unstructured.SetNestedField(m.u.Object, int64(replicas), "spec", "replicas")
}

func (m Manifest) AddVariantLabel(variant string) error {
	var (
		matchLabelsFields = []string{"spec", "selector", "matchLabels"}
		labelsFields      = []string{"spec", "template", "metadata", "labels"}
	)

	// Add variant label into selector.matchLabels.
	matchLabels, _, err := unstructured.NestedStringMap(m.u.Object, matchLabelsFields...)
	if err != nil {
		return err
	}
	if matchLabels == nil {
		matchLabels = make(map[string]string, 1)
	}
	matchLabels[PredefinedLabelVariant] = variant
	if err := unstructured.SetNestedStringMap(m.u.Object, matchLabels, matchLabelsFields...); err != nil {
		return err
	}

	// Add variant label into template label.
	labels, _, err := unstructured.NestedStringMap(m.u.Object, labelsFields...)
	if err != nil {
		return err
	}
	if labels == nil {
		labels = make(map[string]string, 1)
	}
	labels[PredefinedLabelVariant] = variant
	if err := unstructured.SetNestedStringMap(m.u.Object, labels, labelsFields...); err != nil {
		return err
	}

	return nil
}

func (m Manifest) ResourceKey() string {
	return fmt.Sprintf("%s:%s:%s:%s", m.APIVersion, m.Kind, m.Namespace, m.Name)
}

type ResourceKey struct {
	APIVersion string
	Kind       string
	Namespace  string
	Name       string
}

func (k ResourceKey) String() string {
	return fmt.Sprintf("%s:%s:%s:%s", k.APIVersion, k.Kind, k.Namespace, k.Name)
}

func DecodeResourceKey(key string) (ResourceKey, error) {
	parts := strings.Split(key, ":")
	if len(parts) != 4 {
		return ResourceKey{}, fmt.Errorf("malformed key")
	}
	return ResourceKey{
		APIVersion: parts[0],
		Kind:       parts[1],
		Namespace:  parts[2],
		Name:       parts[3],
	}, nil
}

func (e *Executor) loadManifests(ctx context.Context) ([]Manifest, error) {
	switch e.templatingMethod {
	case TemplatingMethodHelm:
		return nil, nil
	case TemplatingMethodKustomize:
		return nil, nil
	case TemplatingMethodNone:
		return loadPlainYAMLMannifests(ctx, e.appDirPath, e.config.Input.Manifests)
	}
	return nil, nil
}

func loadPlainYAMLMannifests(ctx context.Context, dir string, names []string) ([]Manifest, error) {
	// If no name was specified we have to walk the app directory to collect the manifest list.
	if len(names) == 0 {
		err := filepath.Walk(dir, func(path string, f os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if path == dir {
				return nil
			}
			if f.IsDir() {
				return filepath.SkipDir
			}
			ext := filepath.Ext(f.Name())
			if ext != ".yaml" && ext != ".yml" {
				return nil
			}
			if f.Name() == config.DeploymentConfigurationFileName {
				return nil
			}
			names = append(names, f.Name())
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	manifests := make([]Manifest, 0, len(names))
	for _, name := range names {
		path := filepath.Join(dir, name)
		ms, err := loadManifestsFromYAMLFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to load maninifest at %s (%v)", path, err)
		}
		manifests = append(manifests, ms...)
	}

	return manifests, nil
}

func loadManifestsFromYAMLFile(path string) ([]Manifest, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	const seperator = "\n---"
	var (
		parts     = strings.Split(string(data), seperator)
		manifests = make([]Manifest, 0, len(parts))
	)

	for _, part := range parts {
		//	Ignore all the cases where no content between separator.
		part = strings.TrimSpace(part)
		if len(part) == 0 {
			continue
		}
		var obj unstructured.Unstructured
		if err := yaml.Unmarshal([]byte(part), &obj); err != nil {
			return nil, err
		}
		manifests = append(manifests, Manifest{
			APIVersion: obj.GetAPIVersion(),
			Kind:       obj.GetKind(),
			Namespace:  obj.GetNamespace(),
			Name:       obj.GetName(),
			u:          &obj,
		})
	}
	return manifests, nil
}
