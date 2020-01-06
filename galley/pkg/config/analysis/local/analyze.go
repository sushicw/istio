// Copyright 2019 Istio Authors
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

package local

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"

	"github.com/hashicorp/go-multierror"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/mesh/v1alpha1"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/meshcfg"
	"istio.io/istio/galley/pkg/config/processing/snapshotter"
	"istio.io/istio/galley/pkg/config/processing/transformer"
	"istio.io/istio/galley/pkg/config/processor"
	"istio.io/istio/galley/pkg/config/processor/transforms"
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/galley/pkg/config/schema"
	"istio.io/istio/galley/pkg/config/schema/collection"
	"istio.io/istio/galley/pkg/config/schema/snapshots"
	"istio.io/istio/galley/pkg/config/scope"
	"istio.io/istio/galley/pkg/config/source/kube"
	"istio.io/istio/galley/pkg/config/source/kube/apiserver"
	"istio.io/istio/galley/pkg/config/source/kube/inmemory"
	"istio.io/istio/galley/pkg/config/util/kuberesource"
	"istio.io/istio/pkg/config/mesh"
)

const (
	domainSuffix      = "cluster.local"
	meshConfigMapKey  = "mesh"
	meshConfigMapName = "istio"
)

// Patch table
var (
	apiserverNew = apiserver.New
)

// SourceAnalyzer handles local analysis of k8s event sources, both live and file-based
type SourceAnalyzer struct {
	m                    *schema.Metadata
	sources              []precedenceSourceInput
	analyzer             *analysis.CombinedAnalyzer
	transformerProviders transformer.Providers
	namespace            resource.Namespace
	istioNamespace       resource.Namespace

	// Which kube resources are used by this analyzer
	// Derived from metadata and the specified analyzer and transformer providers
	kubeResources collection.Schemas

	// Hook function called when a collection is used in analysis
	collectionReporter snapshotter.CollectionReporterFn
}

// AnalysisResult represents the returnable results of an analysis execution
type AnalysisResult struct {
	Messages          diag.Messages
	SkippedAnalyzers  []string
	ExecutedAnalyzers []string
}

// NewSourceAnalyzer creates a new SourceAnalyzer with no sources. Use the Add*Source methods to add sources in ascending precedence order,
// then execute Analyze to perform the analysis
func NewSourceAnalyzer(m *schema.Metadata, analyzer *analysis.CombinedAnalyzer, namespace, istioNamespace resource.Namespace,
	cr snapshotter.CollectionReporterFn, serviceDiscovery bool) *SourceAnalyzer {

	// collectionReporter hook function defaults to no-op
	if cr == nil {
		cr = func(collection.Name) {}
	}

	transformerProviders := transforms.Providers(m)

	// Get the closure of all input collections for our analyzer, paying attention to transforms
	kubeResources := kuberesource.DisableExcludedCollections(
		m.KubeCollections(),
		transformerProviders,
		analyzer.Metadata().Inputs,
		kuberesource.DefaultExcludedResourceKinds(),
		serviceDiscovery)

	sa := &SourceAnalyzer{
		m:                    m,
		sources:              make([]precedenceSourceInput, 0),
		analyzer:             analyzer,
		transformerProviders: transformerProviders,
		namespace:            namespace,
		istioNamespace:       istioNamespace,
		kubeResources:        kubeResources,
		collectionReporter:   cr,
	}

	sa.addMeshConfigSource(meshcfg.Default())

	return sa
}

// Analyze loads the sources and executes the analysis
func (sa *SourceAnalyzer) Analyze(cancel chan struct{}) (AnalysisResult, error) {
	var result AnalysisResult

	// We need more than the default mesh config we always start with
	if len(sa.sources) <= 1 {
		return result, fmt.Errorf("at least one file and/or Kubernetes source must be provided")
	}

	var namespaces []resource.Namespace
	if sa.namespace != "" {
		namespaces = []resource.Namespace{sa.namespace}
	}

	var colsInSnapshots collection.Names
	for _, c := range sa.m.AllCollectionsInSnapshots([]string{snapshots.LocalAnalysis, snapshots.SyntheticServiceEntry}) {
		colsInSnapshots = append(colsInSnapshots, collection.NewName(c))
	}

	result.SkippedAnalyzers = sa.analyzer.RemoveSkipped(colsInSnapshots, sa.kubeResources.DisabledCollectionNames(),
		sa.transformerProviders)
	result.ExecutedAnalyzers = sa.analyzer.AnalyzerNames()

	updater := &snapshotter.InMemoryStatusUpdater{}
	distributorSettings := snapshotter.AnalyzingDistributorSettings{
		StatusUpdater:      updater,
		Analyzer:           sa.analyzer,
		Distributor:        snapshotter.NewInMemoryDistributor(),
		AnalysisSnapshots:  []string{snapshots.LocalAnalysis, snapshots.SyntheticServiceEntry},
		TriggerSnapshot:    snapshots.LocalAnalysis,
		CollectionReporter: sa.collectionReporter,
		AnalysisNamespaces: namespaces,
	}
	distributor := snapshotter.NewAnalyzingDistributor(distributorSettings)

	processorSettings := processor.Settings{
		Metadata:           sa.m,
		DomainSuffix:       domainSuffix,
		Source:             newPrecedenceSource(sa.sources),
		TransformProviders: sa.transformerProviders,
		Distributor:        distributor,
		EnabledSnapshots:   []string{snapshots.LocalAnalysis, snapshots.SyntheticServiceEntry},
	}
	rt, err := processor.Initialize(processorSettings)
	if err != nil {
		return result, err
	}
	rt.Start()
	defer rt.Stop()

	scope.Analysis.Debugf("Waiting for analysis messages to be available...")
	if updater.WaitForReport(cancel) {
		result.Messages = updater.Get()
		return result, nil
	}

	return result, errors.New("cancelled")
}

// AddReaderKubeSource adds a source based on the specified k8s yaml files to the current SourceAnalyzer
func (sa *SourceAnalyzer) AddReaderKubeSource(readers []io.Reader) error {
	src := inmemory.NewKubeSource(sa.kubeResources)
	src.SetDefaultNamespace(sa.namespace)

	var errs error

	// If we encounter any errors reading or applying files, track them but attempt to continue
	for i, r := range readers {
		by, err := ioutil.ReadAll(r)
		if err != nil {
			errs = multierror.Append(errs, err)
			continue
		}

		if err = src.ApplyContent(string(i), string(by)); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	sa.sources = append(sa.sources, precedenceSourceInput{src: src, cols: sa.kubeResources.CollectionNames()})

	return errs
}

// AddRunningKubeSource adds a source based on a running k8s cluster to the current SourceAnalyzer
// Also adds a meshcfg source from the running cluster
func (sa *SourceAnalyzer) AddRunningKubeSource(k kube.Interfaces) {
	o := apiserver.Options{
		Client:  k,
		Schemas: sa.kubeResources,
	}

	if err := sa.addRunningKubeMeshConfigSource(k); err != nil {
		scope.Analysis.Errorf("error getting mesh config from running kube source (defaults will be used instead): %v", err)
	}

	src := apiserverNew(o)
	sa.sources = append(sa.sources, precedenceSourceInput{src: src, cols: sa.kubeResources.CollectionNames()})
}

// AddFileKubeMeshConfigSource adds a mesh config source based on the specified meshconfig yaml file
func (sa *SourceAnalyzer) AddFileKubeMeshConfigSource(file string) error {
	by, err := ioutil.ReadFile(file)
	if err != nil {
		return err
	}

	cfg, err := mesh.ApplyMeshConfigDefaults(string(by))
	if err != nil {
		return err
	}

	sa.addMeshConfigSource(cfg)

	return nil
}

func (sa *SourceAnalyzer) addRunningKubeMeshConfigSource(k kube.Interfaces) error {
	client, err := k.KubeClient()
	if err != nil {
		return fmt.Errorf("error getting KubeClient: %v", err)
	}

	meshConfigMap, err := client.CoreV1().ConfigMaps(string(sa.istioNamespace)).Get(meshConfigMapName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("could not read configmap %q from namespace %q: %v", meshConfigMapName, sa.istioNamespace, err)
	}

	configYaml, ok := meshConfigMap.Data[meshConfigMapKey]
	if !ok {
		return fmt.Errorf("missing config map key %q", meshConfigMapKey)
	}

	cfg, err := mesh.ApplyMeshConfigDefaults(configYaml)
	if err != nil {
		return fmt.Errorf("error parsing mesh config: %v", err)
	}

	sa.addMeshConfigSource(cfg)

	return nil
}

// addMeshConfigSource adds a mesh config source. The last added source takes precedence over the others, without any sort of merging.
func (sa *SourceAnalyzer) addMeshConfigSource(cfg *v1alpha1.MeshConfig) {
	meshsrc := meshcfg.NewInmemory()
	meshsrc.Set(cfg)
	sa.sources = append(sa.sources, precedenceSourceInput{src: meshsrc, cols: collection.Names{meshcfg.IstioMeshconfig}})
}
