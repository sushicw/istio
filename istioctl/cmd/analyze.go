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

package cmd

import (
	"fmt"
	"path/filepath"

	"istio.io/istio/galley/pkg/config/analysis/local"
	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/processor/metadata"
	"istio.io/istio/galley/pkg/config/source/priority"

	"github.com/spf13/cobra"
)

var (
	useKube bool
)

// Analyze command
func Analyze() *cobra.Command {
	analysisCmd := &cobra.Command{
		Use:   "analyze <file|globpattern>...",
		Short: "Analyze Istio configuration",
		Example: `
# Analyze yaml files
istioctl experimental analyze a.yaml b.yaml

# Analyze the current live cluster
istioctl experimental analyze -k -c $HOME/.kube/config

# Analyze the current live cluster, simulating the effect of applying additional yaml files
istioctl experimental analyze -k -c $HOME/.kube/config a.yaml b.yaml
`,
		RunE: func(cmd *cobra.Command, args []string) error {

			files, err := gatherFiles(args)
			if err != nil {
				return err
			}
			cancel := make(chan struct{})

			m := metadata.MustGet()

			// If we're using kube, use that as a base source.
			var sources = make([]event.Source, 0)
			if useKube {
				src, err := local.GetKubeBasedSource(m, kubeconfig)
				if err != nil {
					return err
				}
				sources = append(sources, src)
			}

			// If files are provided, treat them (collectively) as a source.
			if len(files) > 0 {
				src, err := local.GetFileBasedSource(m, files...)
				if err != nil {
					return err
				}
				sources = append(sources, src)
			}

			if len(sources) == 0 {
				return fmt.Errorf("At least one file and/or kubernetes source must be provided")
			}

			src := priority.New(sources...)

			messages, err := local.AnalyzeSource(m, "svc.local", src, cancel)
			if err != nil {
				return err
			}

			for _, m := range messages {
				fmt.Printf("%v\n", m.String())
			}

			return nil
		},
	}

	analysisCmd.PersistentFlags().BoolVarP(&useKube, "use-kube", "k", false,
		"Use live kubernetes cluster for analysis")

	return analysisCmd
}

func gatherFiles(args []string) ([]string, error) {
	var result []string
	for _, a := range args {
		paths, err := filepath.Glob(a)
		if err != nil {
			return nil, err
		}
		result = append(result, paths...)
	}
	return result, nil
}
