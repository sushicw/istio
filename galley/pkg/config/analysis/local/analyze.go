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
	"io/ioutil"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/analyzers"
	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/meshcfg"
	"istio.io/istio/galley/pkg/config/processing/snapshotter"
	"istio.io/istio/galley/pkg/config/processor"
	"istio.io/istio/galley/pkg/config/schema"
	"istio.io/istio/galley/pkg/config/source/kube/apiserver"
	"istio.io/istio/galley/pkg/config/source/kube/inmemory"
	"istio.io/istio/galley/pkg/source/kube/client"
)

// SourceAnalyzer handles local analysis of k8s and file based event sources
type SourceAnalyzer struct {
	m            *schema.Metadata
	domainSuffix string
	sources      []event.Source
	analyzer     analysis.Analyzer
}

// NewSourceAnalyzer creates a new SourceAnalyzer with no sources. Use the Add*Source methods to add sources in ascending precedence order.
func NewSourceAnalyzer(m *schema.Metadata, domainSuffix string, analyzer analysis.Analyzer) *SourceAnalyzer {
	return &SourceAnalyzer{
		m:            m,
		domainSuffix: domainSuffix,
		sources:      make([]event.Source, 0),
		analyzer:     analyzer,
	}
}

// Analyze loads the sources and executes the analysis
func (sa *SourceAnalyzer) Analyze(cancel chan struct{}) (diag.Messages, error) {

	meshsrc := meshcfg.NewInmemory()
	meshsrc.Set(meshcfg.Default())

	if len(sa.sources) == 0 {
		return nil, fmt.Errorf("At least one file and/or kubernetes source must be provided")
	}
	src := newPrecedenceSource(sa.sources)

	updater := &snapshotter.InMemoryStatusUpdater{}
	distributor := snapshotter.NewAnalyzingDistributor(updater, analyzers.All(), snapshotter.NewInMemoryDistributor())
	rt, err := processor.Initialize(sa.m, sa.domainSuffix, event.CombineSources(src, meshsrc), distributor)
	if err != nil {
		return nil, err
	}
	rt.Start()
	defer rt.Stop()

	if updater.WaitForReport(cancel) {
		return updater.Get(), nil
	}

	return nil, errors.New("cancelled")
}

func (sa *SourceAnalyzer) AddFileBasedSource(files []string) error {
	src := inmemory.NewKubeSource(sa.m.KubeSource().Resources())

	for _, file := range files {
		by, err := ioutil.ReadFile(file)
		if err != nil {
			return err
		}
		if err = src.ApplyContent(file, string(by)); err != nil {
			return err
		}
	}

	sa.sources = append(sa.sources, src)
	return nil
}

func (sa *SourceAnalyzer) AddKubeBasedSource(kubeconfig string) error {
	k, err := client.NewKubeFromConfigFile(kubeconfig)
	if err != nil {
		return err
	}

	o := apiserver.Options{
		Client:    k,
		Resources: sa.m.KubeSource().Resources(),
	}
	src := apiserver.New(o)

	sa.sources = append(sa.sources, src)
	return nil
}
