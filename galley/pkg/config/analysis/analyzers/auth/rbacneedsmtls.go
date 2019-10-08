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

package auth

import (
	"fmt"

	"istio.io/api/rbac/v1alpha1"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/util"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/galley/pkg/config/meta/metadata"
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/resource"
)

// RbacNeedsMtlsAnalyzer checks that if RBAC is enabled, is also enabled.
// RBAC requires mTLS to function correctly.
// TODO: Add to all()
type RbacNeedsMtlsAnalyzer struct{}

var _ analysis.Analyzer = &RbacNeedsMtlsAnalyzer{}

// Metadata implements Analyzer
func (a *RbacNeedsMtlsAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name: "auth.RbacNeedsMtlsAnalyzer",
		Inputs: collection.Names{
			metadata.IstioRbacV1Alpha1Clusterrbacconfigs,
		},
	}
}

// Analyze implements Analyzer
func (a *RbacNeedsMtlsAnalyzer) Analyze(ctx analysis.Context) {
	// Get the singleton cluster RBAC config
	var crbEntry *resource.Entry
	ctx.ForEach(metadata.IstioRbacV1Alpha1Clusterrbacconfigs, func(r *resource.Entry) bool {
		// Should be exactly one cluster rbac config
		if crbEntry != nil {
			ctx.Report(metadata.IstioRbacV1Alpha1Clusterrbacconfigs, msg.NewNotYetImplemented(r, fmt.Sprintf("TODO: Proper warning message more than one cluster rbac config was found")))
			return true
		}
		crbEntry = r
		return true
	})

	// Get the set of services for which RBAC is enabled / disabled
	servicesWithRbac := getServicesWithRbac(crbEntry, ctx)
	if servicesWithRbac == nil {
		// Either RBAC is off or we had an internal error. Either way, exit the analyzer.
		return
	}

	// Get services with mTLS enabled
	servicesWithMtls := getServicesWithMtls(ctx)

	// Verify each service with RBAC also has mTLS
	for svc := range servicesWithRbac {
		if _, ok := servicesWithMtls[svc]; !ok {
			//TODO: create new validation message for this
			ctx.Report(metadata.IstioRbacV1Alpha1Clusterrbacconfigs, msg.NewNotYetImplemented(crbEntry, fmt.Sprintf("Service %q has RBAC enabled, but does not have mTLS enabled.", svc)))
		}
	}
}

func getServicesWithRbac(crbEntry *resource.Entry, ctx analysis.Context) map[resource.Name]struct{} {
	crb := crbEntry.Item.(*v1alpha1.RbacConfig)

	switch crb.Mode {
	case v1alpha1.RbacConfig_OFF:
		return nil
	case v1alpha1.RbacConfig_ON_WITH_INCLUSION:
		return getServicesFromTarget(ctx, crb.GetInclusion())
	case v1alpha1.RbacConfig_ON:
		return getAllServicesExcept(ctx, nil)
	case v1alpha1.RbacConfig_ON_WITH_EXCLUSION:
		return getAllServicesExcept(ctx, crb.GetExclusion())
	default:
		ctx.Report(metadata.IstioRbacV1Alpha1Clusterrbacconfigs, msg.NewInternalError(crbEntry, fmt.Sprintf("Unexpected mode in cluster rbac config: %v", crb.Mode)))
		return nil
	}
}

func getServicesFromTarget(ctx analysis.Context, t *v1alpha1.RbacConfig_Target) map[resource.Name]struct{} {
	result := make(map[resource.Name]struct{})
	for _, svc := range t.GetServices() {
		result[util.GetResourceNameFromHost("", svc)] = struct{}{}
	}
	for _, ns := range t.GetNamespaces() {
		for _, svc := range getServicesInNamespace(ctx, ns) {
			result[util.GetResourceNameFromHost("", svc)] = struct{}{}
		}
	}
	return result
}

//TODO: Do we need to look at serviceEntry hosts here as well? (does RBAC work for manually / externally defined services?)
func getServicesInNamespace(ctx analysis.Context, namespace string) []string {
	result := make([]string, 0)
	ctx.ForEach(metadata.IstioNetworkingV1Alpha3SyntheticServiceentries, func(r *resource.Entry) bool {
		ns, name := r.Metadata.Name.InterpretAsNamespaceAndName()
		if ns == namespace {
			result = append(result, name)
		}
		return true
	})
	return result
}

//TODO: Do we need to look at serviceEntry hosts here as well? (does RBAC work for manually / externally defined services?)
func getAllServicesExcept(ctx analysis.Context, t *v1alpha1.RbacConfig_Target) map[resource.Name]struct{} {
	excludeServices := getServicesFromTarget(ctx, t)

	result := make(map[resource.Name]struct{})
	ctx.ForEach(metadata.IstioNetworkingV1Alpha3SyntheticServiceentries, func(r *resource.Entry) bool {
		if _, ok := excludeServices[r.Metadata.Name]; !ok {
			result[r.Metadata.Name] = struct{}{}
		}
		return true
	})
	return result
}

func getServicesWithMtls(ctx analysis.Context) map[resource.Name]struct{} {
	result := make(map[resource.Name]struct{})
	//TODO
	return result
}
