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

package priority

import (
	"sync"

	"istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/galley/pkg/config/event"
)

// Source is a processor.Source implementation that combines multiple sources in priority order
// Such that events from sources later in the input list take priority over events affecting
// the same resource from sources earlier in the list
type Source struct {
	mu      sync.Mutex
	started bool

	inputs  []event.Source
	handler event.Handler

	eventStateMu    sync.Mutex
	eventPriorities map[string]int
	fullSyncCounts  map[collection.Name]int
}

type priorityHandler struct {
	priority int
	src      *Source
}

var _ event.Source = &Source{}

// New returns a new priority source, based on given input sources.
func New(sources ...event.Source) *Source {
	return &Source{
		inputs:          sources,
		eventPriorities: make(map[string]int),
		fullSyncCounts:  make(map[collection.Name]int),
	}
}

// Handle implements event.Handler
func (ph *priorityHandler) Handle(e event.Event) {
	ph.src.eventStateMu.Lock()
	defer ph.src.eventStateMu.Unlock()

	if e.Kind == event.FullSync {
		ph.handleFullSync(e)
	} else {
		ph.handleEvent(e)
	}
}

// handleFullSync handles FullSync events, which are a special case.
// For each collection, we want to only send this once, after all upstream sources have sent theirs.
func (ph *priorityHandler) handleFullSync(e event.Event) {
	ph.src.fullSyncCounts[e.Source]++
	if ph.src.fullSyncCounts[e.Source] != len(ph.src.inputs) {
		return
	}
	ph.src.handler.Handle(e)
}

// handleEvent handles non fullsync events.
// For each event, only pass it along to the downstream handler if the source it came from had equal or higher priority
func (ph *priorityHandler) handleEvent(e event.Event) {
	curPriority, ok := ph.src.eventPriorities[e.String()]
	if ok && ph.priority < curPriority {
		return
	}
	ph.src.eventPriorities[e.String()] = ph.priority
	ph.src.handler.Handle(e)
}

// Dispatch implements event.Source
func (s *Source) Dispatch(h event.Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.handler = h

	// Inject a PriorityHandler for each source
	// Priority is based on index position (higher index, higher priority)
	for i, input := range s.inputs {
		ph := &priorityHandler{
			priority: i,
			src:      s,
		}
		input.Dispatch(ph)
	}
}

// Start implements processor.Source
func (s *Source) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return
	}

	for _, i := range s.inputs {
		i.Start()
	}

	s.started = true
}

// Stop implements processor.Source
func (s *Source) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return
	}

	s.started = false

	for _, i := range s.inputs {
		i.Stop()
	}
}
