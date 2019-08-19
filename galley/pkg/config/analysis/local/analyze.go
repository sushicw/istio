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

	"istio.io/pkg/log"

	"istio.io/istio/galley/pkg/config/analysis/analyzers"
	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/meshcfg"
	"istio.io/istio/galley/pkg/config/processing/snapshotter"
	"istio.io/istio/galley/pkg/config/processor"
	"istio.io/istio/galley/pkg/config/schema"
	"istio.io/istio/galley/pkg/config/source/kube"
	"istio.io/istio/galley/pkg/config/source/kube/apiserver"
	"istio.io/istio/galley/pkg/config/source/kube/inmemory"
	"istio.io/istio/galley/pkg/source/kube/client"
)

// AnalyzeSource handles local analysis of an event source (e.g. file based, k8s based, or combined)
// TODO: What do I need to do to properly handle domainSuffix?
func AnalyzeSource(m *schema.Metadata, domainSuffix string, src event.Source, cancel chan struct{}) (diag.Messages, error) {
	o := log.DefaultOptions()
	o.SetOutputLevel("processing", log.ErrorLevel)
	o.SetOutputLevel("source", log.ErrorLevel)
	err := log.Configure(o)
	if err != nil {
		return nil, fmt.Errorf("unable to configure logging: %v", err)
	}

	meshsrc := meshcfg.NewInmemory()
	meshsrc.Set(meshcfg.Default())

	updater := &snapshotter.InMemoryStatusUpdater{}
	distributor := snapshotter.NewAnalyzingDistributor(updater, analyzers.All(), snapshotter.NewInMemoryDistributor())
	rt, err := processor.Initialize(m, domainSuffix, event.CombineSources(src, meshsrc), distributor)
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

func GetFileBasedSource(m *schema.Metadata, files []string) (event.Source, error) {
	src := inmemory.NewKubeSource(m.KubeSource().Resources())
	for _, file := range files {
		by, err := ioutil.ReadFile(file)
		if err != nil {
			return nil, err
		}
		if err = src.ApplyContent(file, string(by)); err != nil {
			return nil, err
		}
	}

	return src, nil
}

func GetKubeBasedSource(m *schema.Metadata, kubeconfig string) (
	src event.Source, err error) {
	resources := m.KubeSource().Resources()

	var k kube.Interfaces
	if k, err = client.NewKubeFromConfigFile(kubeconfig); err != nil {
		return
	}

	o := apiserver.Options{
		Client:    k,
		Resources: resources,
	}
	s := apiserver.New(o)
	src = s

	return
}
