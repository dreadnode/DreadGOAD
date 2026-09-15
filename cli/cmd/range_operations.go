package cmd

import (
	"fmt"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/rangeconfig"
)

// rangeOperations centralizes the non-path behavior selected by a range.
// Commands consume capabilities from this value instead of branching on a
// range directory name.
type rangeOperations struct {
	persistentAzureState    bool
	azureProvisionViaAttack bool
	autoBootstrapAWSBackend bool
	validateServiceInfra    bool
}

var rangeOperationProfiles = map[string]rangeOperations{
	rangeconfig.ProfileActiveDir: {},
	rangeconfig.ProfileGOAT: {
		persistentAzureState:    true,
		azureProvisionViaAttack: true,
		autoBootstrapAWSBackend: true,
		validateServiceInfra:    true,
	},
}

func operationsFor(cfg *config.Config) (rangeOperations, error) {
	manifest, found, err := rangeconfig.Load(cfg.LabPath())
	if err != nil {
		return rangeOperations{}, err
	}
	profile := rangeconfig.ProfileActiveDir
	if found {
		profile = manifest.Operations.Profile
		if profile == "" {
			if manifest.Kind == rangeconfig.KindActiveDirectory {
				profile = rangeconfig.ProfileActiveDir
			} else {
				return rangeOperations{}, fmt.Errorf(
					"range %s must declare operations.profile", cfg.ResolvedLab(),
				)
			}
		}
	}
	operations, ok := rangeOperationProfiles[profile]
	if !ok {
		return rangeOperations{}, fmt.Errorf(
			"range %s uses unsupported operations profile %q", cfg.ResolvedLab(), profile,
		)
	}
	return operations, nil
}
