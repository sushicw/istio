package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kiali/kiali/business"
)

var (
	kialiCheckCmd = &cobra.Command{
		Use:   "kialicheck",
		Short: "kialicheck",
		Long: `
TODO: Long
`,
		Example: `
TODO: Example
`,
		// nolint: goimports
		Args: cobra.NoArgs,
		RunE: runKialiCheck,
		//DisableFlagsInUseLine: true,
	}
)

func runKialiCheck(c *cobra.Command, args []string) error {
	businessLayer, err := business.Get("")
	if err != nil {
		return fmt.Errorf("error getting business layer: %v", err)
		// Problem: the above assumes we're already inside the cluster.
		// "unable to load in-cluster configuration, KUBERNETES_SERVICE_HOST and KUBERNETES_SERVICE_PORT must be defined"
		// Workaround?
		//     ```kubectl proxy --port=8080```
		//     ```KUBERNETES_SERVICE_HOST=localhost KUBERNETES_SERVICE_PORT=8080 ~/go/out/linux_amd64/release/istioctl experimental kialicheck```
	}

	istioConfigValidationResults, err := businessLayer.Validations.GetValidations("default", "")
	if err != nil {
		return fmt.Errorf("error getting validation results: %v", err)
		// Problem: Getting "Unauthorized" HTTP response code somewhere in the above. Why?
		// proxy not working somehow? Works when I curl it directly...
		// Solution: leave the token blank in business.Get
		// it's expecting a k8s token. If left blank, kubectl proxy uses what's in the user's file, all is well.
	}

	for _, v := range istioConfigValidationResults {
		if !v.Valid {
			fmt.Printf("%s '%s' has %d problems:\n", v.ObjectType, v.Name, len(v.Checks))
			for _, c := range v.Checks {
				fmt.Printf("\t%s: %s <%s>\n", c.Severity, c.Message, c.Path)
				//TODO: Print the path info more beautifully
			}
		}
	}
	return nil

	//TODO: Try from Galley?
}
