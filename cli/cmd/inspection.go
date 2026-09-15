package cmd

import (
	"context"
	"fmt"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/rangeconfig"
	"github.com/spf13/cobra"
)

// labInspector keeps the user-facing inspection commands stable while each
// lab family implements the checks that are meaningful for its workloads.
type labInspector interface {
	health(context.Context, *cobra.Command, *config.Config, bool) error
	validate(context.Context, *cobra.Command, *config.Config, validateOpts) error
}

const inspectionProfileActiveDirectory = "active-directory"

var inspectionProfiles = map[string]labInspector{
	inspectionProfileActiveDirectory: goadInspector{},
}

func inspectorFor(cfg *config.Config) (labInspector, error) {
	manifest, found, err := rangeconfig.Load(cfg.LabPath())
	if err != nil {
		return nil, err
	}
	profile := inspectionProfileActiveDirectory
	if found {
		profile = manifest.Inspection.Profile
		if profile == "" {
			if manifest.Kind == rangeconfig.KindActiveDirectory {
				profile = inspectionProfileActiveDirectory
			} else {
				return nil, fmt.Errorf("range %s must declare inspection.profile", cfg.ResolvedLab())
			}
		}
	}
	inspector, ok := inspectionProfiles[profile]
	if !ok {
		return nil, fmt.Errorf("range %s uses unsupported inspection profile %q", cfg.ResolvedLab(), profile)
	}
	return inspector, nil
}

type goadInspector struct{}

func (goadInspector) health(
	ctx context.Context, cmd *cobra.Command, cfg *config.Config, jsonOut bool,
) error {
	return runGOADHealthCheck(ctx, cmd, cfg, jsonOut)
}

func (goadInspector) validate(
	ctx context.Context, cmd *cobra.Command, cfg *config.Config, opts validateOpts,
) error {
	return runGOADValidate(ctx, cmd, cfg, opts)
}
