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
	"fmt"
	"sync"

	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/pkg/log"
)

// Source is a processor.Source implementation that combines multiple sources in priority order
// Such that sources later in the list take priority of sources earlier in the list
//TODO: Move this elsewhere / rename?
//TODO: Refactor? prorityHandler having a pointer to src seems particularly ugly
type Source struct {
	mu      sync.Mutex
	started bool

	inputs  []event.Source
	handler event.Handler
	ep      map[string]int
}

type priorityHandler struct {
	priority int
	src      *Source
}

var _ event.Source = &Source{}

// New returns a new priority source, based on given input sources.
func New(sources ...event.Source) *Source {
	return &Source{
		inputs: sources,
		ep:     make(map[string]int),
	}
}

// Handle implements event.Handler
// For each event, only pass it along to the downstream handler if the source it came from had equal or higher priority
//TODO: Still something nondeterministic to hammer out
func (ph *priorityHandler) Handle(e event.Event) {
	//TODO: Leaving this in causes everything to hang when using files as a source (but not Kube?)
	// ph.src.mu.Lock()
	// defer ph.src.mu.Unlock()

	key := getEventKey(e)

	curPriority, ok := ph.src.ep[key]
	log.Errorf("DEBUG0a: %v %s %d %t %d %t", e, key, ph.priority, ok, curPriority, (!ok || ph.priority >= curPriority))
	if !ok || ph.priority >= curPriority {
		ph.src.ep[key] = ph.priority
		ph.src.handler.Handle(e)
	} else {
	}
}

func getEventKey(e event.Event) string {
	var entryName resource.Name
	if e.Kind == event.FullSync {
		// e.Entry isn't defined for FullSync events, so we use this instead
		entryName = resource.NewName("", "FullSync")
	} else {
		entryName = e.Entry.Metadata.Name
	}

	return fmt.Sprintf("%s/%s", e.Source, entryName)
}

// Dispatch implements event.Source
func (s *Source) Dispatch(h event.Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.handler = h

	// Inject a PriorityHandler for each source
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
