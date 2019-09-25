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

package snapshotter

import (
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/processing"
	"istio.io/istio/galley/pkg/config/processing/transformer"
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/galley/pkg/config/testing/data"
	"istio.io/istio/pkg/mcp/snapshot"
)

type updaterMock struct{}

// Update implements StatusUpdater
func (u *updaterMock) Update(messages diag.Messages) {}

type analyzerMock struct {
	analyzeCalls       []*Snapshot
	collectionToAccess collection.Name
}

// Analyze implements Analyzer
func (a *analyzerMock) Analyze(c analysis.Context) {
	ctx := *c.(*context)

	a.analyzeCalls = append(a.analyzeCalls, ctx.sn)

	ctx.Exists(a.collectionToAccess, resource.NewName("", ""))
}

// Name implements Analyzer
func (a *analyzerMock) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:   "",
		Inputs: collection.Names{},
	}
}

func TestAnalyzeAndDistributeSnapshots(t *testing.T) {
	g := NewGomegaWithT(t)

	u := &updaterMock{}
	a := &analyzerMock{
		collectionToAccess: data.Collection1,
	}
	d := NewInMemoryDistributor()

	var collectionAccessed collection.Name
	cr := func(col collection.Name) {
		collectionAccessed = col
	}

	ad := NewAnalyzingDistributor(u, analysis.Combine("testCombined", a), d, cr, collection.Names{}, transformer.Providers{})

	sDefault := getTestSnapshot("a", "b")
	sSynthetic := getTestSnapshot("c")
	sOther := getTestSnapshot("a", "d")

	ad.Distribute(syntheticSnapshotGroup, sSynthetic)
	ad.Distribute(defaultSnapshotGroup, sDefault)
	ad.Distribute("other", sOther)

	// Assert we sent every received snapshot to the distributor
	g.Eventually(func() snapshot.Snapshot { return d.GetSnapshot(syntheticSnapshotGroup) }).Should(Equal(sSynthetic))
	g.Eventually(func() snapshot.Snapshot { return d.GetSnapshot(defaultSnapshotGroup) }).Should(Equal(sDefault))
	g.Eventually(func() snapshot.Snapshot { return d.GetSnapshot("other") }).Should(Equal(sOther))

	// Assert we triggered analysis only once, with the expected combination of snapshots
	sCombined := getTestSnapshot("a", "b", "c")
	g.Eventually(func() []*Snapshot { return a.analyzeCalls }).Should(ConsistOf(sCombined))

	// Verify the collection reporter hook was called
	g.Expect(collectionAccessed).To(Equal(a.collectionToAccess))
}

func getTestSnapshot(names ...string) *Snapshot {
	c := make([]*collection.Instance, 0)
	for _, name := range names {
		c = append(c, collection.New(collection.NewName(name)))
	}
	return &Snapshot{
		set: collection.NewSetFromCollections(c),
	}
}

func TestGetDisabledOutputs(t *testing.T) {
	g := NewGomegaWithT(t)

	in1 := collection.NewName("in1")
	in2 := collection.NewName("in2")
	in3 := collection.NewName("in3")
	in4 := collection.NewName("in4")
	in5 := collection.NewName("in5")
	out1 := collection.NewName("out1")
	out2 := collection.NewName("out2")
	out3 := collection.NewName("out3")
	out4 := collection.NewName("out4")

	blankFn := func(_ processing.ProcessorOptions) event.Transformer {
		return event.NewFnTransform(collection.Names{}, collection.Names{}, func() {}, func() {}, func(e event.Event, handler event.Handler) {})
	}

	xformProviders := transformer.Providers{
		transformer.NewProvider(collection.Names{in1}, collection.Names{out1, out2}, blankFn),
		transformer.NewProvider(collection.Names{in2}, collection.Names{out3}, blankFn),
		transformer.NewProvider(collection.Names{in3}, collection.Names{out3}, blankFn),
		transformer.NewProvider(collection.Names{in4, in5}, collection.Names{out4}, blankFn),
	}

	g.Expect(getDisabledOutputs(collection.Names{in1}, xformProviders)).To(ConsistOf(out1, out2))
	g.Expect(getDisabledOutputs(collection.Names{in2}, xformProviders)).To(ConsistOf())
	g.Expect(getDisabledOutputs(collection.Names{in2, in3}, xformProviders)).To(ConsistOf(out3))
	g.Expect(getDisabledOutputs(collection.Names{in4}, xformProviders)).To(ConsistOf(out4))
}
