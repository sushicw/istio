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

package virtualservice

import (
	"strings"

	"istio.io/api/networking/v1alpha3"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/util"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/galley/pkg/config/meta/metadata"
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/resource"
)

// DestinationHostAnalyzer checks the destination hosts associated with each virtual service
type DestinationHostAnalyzer struct{}

var _ analysis.Analyzer = &DestinationHostAnalyzer{}

type hostAndSubset struct {
	host   resource.Name
	subset string
}

// Metadata implements Analyzer
func (d *DestinationHostAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name: "virtualservice.DestinationHostAnalyzer",
		Inputs: collection.Names{
			metadata.IstioNetworkingV1Alpha3SyntheticServiceentries,
			metadata.IstioNetworkingV1Alpha3Serviceentries,
			metadata.IstioNetworkingV1Alpha3Virtualservices,
		},
	}
}

// Analyze implements Analyzer
func (d *DestinationHostAnalyzer) Analyze(ctx analysis.Context) {
	// Precompute the set of service entry hosts that exist (there can be more than one defined per ServiceEntry CRD)
	serviceEntryHosts, wildcardHosts := initServiceEntryHostNames(ctx)

	ctx.ForEach(metadata.IstioNetworkingV1Alpha3Virtualservices, func(r *resource.Entry) bool {
		d.analyzeVirtualService(r, ctx, serviceEntryHosts, wildcardHosts)
		return true
	})
}

func (d *DestinationHostAnalyzer) analyzeVirtualService(r *resource.Entry, ctx analysis.Context,
	serviceEntryHosts map[util.ScopedFqdn]bool, wildcardHosts []util.ScopedFqdn) {

	vs := r.Item.(*v1alpha3.VirtualService)
	ns, _ := r.Metadata.Name.InterpretAsNamespaceAndName()

	destinations := getRouteDestinations(vs)

	for _, destination := range destinations {
		if !d.checkDestinationHost(ns, destination, ctx, serviceEntryHosts, wildcardHosts) {
			ctx.Report(metadata.IstioNetworkingV1Alpha3Virtualservices,
				msg.NewReferencedResourceNotFound(r, "host", destination.GetHost()))
		}
	}
}

func (d *DestinationHostAnalyzer) checkDestinationHost(vsNamespace string, destination *v1alpha3.Destination,
	ctx analysis.Context, serviceEntryHosts map[util.ScopedFqdn]bool, wildcardHosts []util.ScopedFqdn) bool {
	host := destination.GetHost()

	// Check explicitly defined ServiceEntries as well as services discovered from the platform

	// ServiceEntries can be either namespace scoped or exposed to all namespaces
	nsScopedFqdn := util.NewScopedFqdn(vsNamespace, vsNamespace, host)
	if _, ok := serviceEntryHosts[nsScopedFqdn]; ok {
		return true
	}

	// Check ServiceEntries which are exposed to all namespaces
	allNsScopedFqdn := util.NewScopedFqdn(util.ExportToAllNamespaces, vsNamespace, host)
	if _, ok := serviceEntryHosts[allNsScopedFqdn]; ok {
		return true
	}

	name := util.GetResourceNameFromHost(vsNamespace, host)
	if ctx.Exists(metadata.IstioNetworkingV1Alpha3SyntheticServiceentries, name) {
		return true
	}

	// Now check wildcard matches, namespace scoped or all namespaces
	for _, seHostScopedFqdn := range wildcardHosts {
		ns, seHost := seHostScopedFqdn.GetScopedNamespaceAndName()

		if ns != util.ExportToAllNamespaces && ns != vsNamespace {
			continue
		}

		seHostWithoutWildcard := strings.TrimPrefix(seHost, "*")
		hostWithoutWildCard := strings.TrimPrefix(host, "*")

		if strings.HasSuffix(hostWithoutWildCard, seHostWithoutWildcard) {
			return true
		}
	}

	return false
}

func initServiceEntryHostNames(ctx analysis.Context) (map[util.ScopedFqdn]bool, []util.ScopedFqdn) {
	hosts := make(map[util.ScopedFqdn]bool)
	wildcardHosts := make([]util.ScopedFqdn, 0)
	ctx.ForEach(metadata.IstioNetworkingV1Alpha3Serviceentries, func(r *resource.Entry) bool {
		s := r.Item.(*v1alpha3.ServiceEntry)
		ns, _ := r.Metadata.Name.InterpretAsNamespaceAndName()
		hostsNamespaceScope := ns
		if util.IsExportToAllNamespaces(s.ExportTo) {
			hostsNamespaceScope = util.ExportToAllNamespaces
		}
		for _, h := range s.GetHosts() {
			scopedHost := util.NewScopedFqdn(hostsNamespaceScope, ns, h)
			if strings.HasPrefix(h, "*") {
				wildcardHosts = append(wildcardHosts, scopedHost)
			}
			hosts[scopedHost] = true // Add wildcards for exact matches also
		}
		return true
	})
	return hosts, wildcardHosts
}
