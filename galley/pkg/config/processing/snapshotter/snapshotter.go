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
	"fmt"
	"sync"
	"time"

	"istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/processing/snapshotter/strategy"
	"istio.io/istio/galley/pkg/config/scope"
	"istio.io/istio/galley/pkg/runtime/monitoring"
)

// Snapshotter is a processor that handles input events and creates snapshotImpl collections.
type Snapshotter struct {
	accumulators      map[collection.Name]*accumulator
	selector          event.Router
	xforms            []event.Transformer
	settings          []SnapshotOptions
	groupSyncCounters []*groupSyncCounter

	// lastEventTime records the last time an event was received.
	lastEventTime time.Time

	// pendingEvents counts the number of events awaiting publishing.
	pendingEvents int64

	// lastSnapshotTime records the last time a snapshotImpl was published.
	lastSnapshotTime time.Time
}

var _ event.Processor = &Snapshotter{}

// HandlerFn handles generated snapshots
type HandlerFn func(*collection.Set)

type accumulator struct {
	reqSyncCount     int
	syncCount        int
	groupSyncCounter *groupSyncCounter
	collection       *collection.Instance
	strategies       []strategy.Instance
}

// groupSyncCounter keeps track of the collections in a snapshot group that have been fully synced,
// as well as how many are still remaining
// We want to allow all collections in the group to have at least received a FullSync before allowing
// a snapshot to get published, so we keep track of which per-collection accumulators have already
// received a FullSync event and how many more we're expecting.
type groupSyncCounter struct {
	mu        sync.Mutex
	remaining int
	synced    map[*collection.Instance]bool
}

// Handle implements event.Handler
func (a *accumulator) Handle(e event.Event) {
	switch e.Kind {
	case event.Added, event.Updated:
		a.collection.Set(e.Entry)
		monitoring.RecordStateTypeCount(e.Source.String(), a.collection.Size())
	case event.Deleted:
		a.collection.Remove(e.Entry.Metadata.Name)
		monitoring.RecordStateTypeCount(e.Source.String(), a.collection.Size())
	case event.FullSync:
		a.syncCount++
	default:
		panic(fmt.Errorf("accumulator.Handle: unhandled event type: %v", e.Kind))
	}

	// Update the group sync counter if we received all required FullSync events for a collection
	gsc := a.groupSyncCounter
	gsc.mu.Lock()
	defer gsc.mu.Unlock()
	if a.syncCount >= a.reqSyncCount && !gsc.synced[a.collection] {
		gsc.remaining--
		gsc.synced[a.collection] = true
	}

	// proceed with triggering the strategy OnChange only after we've full synced every collection in this group.
	if gsc.remaining == 0 {
		for _, s := range a.strategies {
			s.OnChange()
		}
	}
}

func (a *accumulator) reset() {
	a.syncCount = 0
	a.collection.Clear()
}

// NewSnapshotter returns a new Snapshotter.
func NewSnapshotter(xforms []event.Transformer, settings []SnapshotOptions) (*Snapshotter, error) {
	s := &Snapshotter{
		accumulators:  make(map[collection.Name]*accumulator),
		selector:      event.NewRouter(),
		xforms:        xforms,
		settings:      settings,
		lastEventTime: time.Now(),
	}

	for _, xform := range xforms {
		for _, i := range xform.Inputs() {
			s.selector = event.AddToRouter(s.selector, i, xform)
		}

		for _, o := range xform.Outputs() {
			a, found := s.accumulators[o]
			if !found {
				a = &accumulator{
					collection: collection.New(o),
				}
				s.accumulators[o] = a
			}
			a.reqSyncCount++
			xform.DispatchFor(o, a)
		}
	}

	for _, o := range settings {
		gsc := newGroupSyncCounter(len(o.Collections))
		s.groupSyncCounters = append(s.groupSyncCounters, gsc)

		for _, c := range o.Collections {
			a := s.accumulators[c]
			if a == nil {
				return nil, fmt.Errorf("unrecognized collection in SnapshotOptions: %v (Group: %s)", c, o.Group)
			}

			a.strategies = append(a.strategies, o.Strategy)
			a.groupSyncCounter = gsc
		}
	}

	return s, nil
}

func newGroupSyncCounter(size int) *groupSyncCounter {
	return &groupSyncCounter{
		synced:    make(map[*collection.Instance]bool),
		remaining: size,
	}
}

// Start implements Processor
func (s *Snapshotter) Start() {
	for _, x := range s.xforms {
		x.Start()
	}

	for _, o := range s.settings {
		// Capture the iteration variable in a local
		opt := o
		o.Strategy.Start(func() {
			s.publish(opt)
		})
	}
}

func (s *Snapshotter) publish(o SnapshotOptions) {
	var collections []*collection.Instance

	for _, n := range o.Collections {
		col := s.accumulators[n].collection.Clone()
		collections = append(collections, col)
	}

	set := collection.NewSetFromCollections(collections)
	sn := &Snapshot{set: set}

	now := time.Now()
	monitoring.RecordProcessorSnapshotPublished(s.pendingEvents, now.Sub(s.lastSnapshotTime))
	s.lastSnapshotTime = now
	s.pendingEvents = 0
	scope.Processing.Infoa("Publishing snapshot for group: ", o.Group)
	scope.Processing.Debuga(sn)
	o.Distributor.Distribute(o.Group, sn)
}

// Stop implements Processor
func (s *Snapshotter) Stop() {
	for i, o := range s.settings {
		o.Strategy.Stop()
		s.groupSyncCounters[i] = newGroupSyncCounter(len(o.Collections))
	}

	for _, x := range s.xforms {
		x.Stop()
	}

	for _, a := range s.accumulators {
		a.reset()
	}
}

// Handle implements Processor
func (s *Snapshotter) Handle(e event.Event) {
	now := time.Now()
	monitoring.RecordProcessorEventProcessed(now.Sub(s.lastEventTime))
	s.lastEventTime = now
	s.pendingEvents++
	s.selector.Handle(e)
}
