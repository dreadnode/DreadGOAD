package ansible

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	daws "github.com/dreadnode/dreadgoad/internal/aws"
	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/inventory"
)

// RetryOptions configures the retry behavior for playbook execution.
type RetryOptions struct {
	Playbook    string
	Env         string
	Inventories []string          // additional inventory paths
	ExtraVars   map[string]string // extra variables passed to ansible-playbook
	Limit       string
	Debug       bool
	MaxRetries  int
	RetryDelay  time.Duration
	LogFile     string
	Log         *slog.Logger // optional; falls back to slog.Default()
}

func (o *RetryOptions) logger() *slog.Logger {
	if o.Log != nil {
		return o.Log
	}
	return slog.Default()
}

// RunPlaybookWithRetry runs a playbook with error-specific retry logic.
func RunPlaybookWithRetry(ctx context.Context, opts RetryOptions) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}
	log := opts.logger()

	if opts.MaxRetries == 0 {
		opts.MaxRetries = cfg.MaxRetries
	}
	if opts.RetryDelay == 0 {
		opts.RetryDelay = time.Duration(cfg.RetryDelay) * time.Second
	}

	for attempt := range opts.MaxRetries {
		if attempt > 0 {
			log.Info("retry attempt", "attempt", attempt, "playbook", opts.Playbook)
			log.Info("waiting before retry", "delay", opts.RetryDelay)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(opts.RetryDelay):
			}
		}

		log.Info("starting playbook", "playbook", opts.Playbook, "attempt", attempt+1, "max", opts.MaxRetries)

		result := RunPlaybook(ctx, RunOptions{
			Playbook:    opts.Playbook,
			Env:         opts.Env,
			Inventories: opts.Inventories,
			Limit:       opts.Limit,
			Debug:       opts.Debug,
			LogFile:     opts.LogFile,
			ExtraVars:   opts.ExtraVars,
		})

		if result.TimedOut {
			log.Error("playbook timed out (idle timeout)", "playbook", opts.Playbook)
			cleanupSSMSessions(ctx, opts.Env, log)
			continue
		}

		if result.Success {
			log.Info("playbook completed successfully", "playbook", opts.Playbook)
			return nil
		}

		log.Warn("playbook failed", "playbook", opts.Playbook,
			"error_type", result.ErrorType, "detail", result.ErrorDetail,
			"failed_hosts", result.FailedHosts)

		retryResult := retryWithErrorStrategy(ctx, opts, result, log)
		if retryResult != nil && retryResult.Success {
			log.Info("playbook succeeded after error-specific retry", "playbook", opts.Playbook)
			return nil
		}
	}

	return fmt.Errorf("playbook %s failed after %d attempts", opts.Playbook, opts.MaxRetries)
}

func retryWithErrorStrategy(ctx context.Context, opts RetryOptions, failResult *RunResult, log *slog.Logger) *RunResult {
	failedHostsStr := strings.Join(failResult.FailedHosts, ",")
	limit := buildRetryLimit(opts.Limit, failedHostsStr)

	baseOpts := RunOptions{
		Playbook:    opts.Playbook,
		Env:         opts.Env,
		Inventories: opts.Inventories,
		Limit:       limit,
		Debug:       opts.Debug,
		LogFile:     opts.LogFile,
		ExtraVars:   opts.ExtraVars,
	}

	switch failResult.ErrorType {
	case ErrFactGathering:
		log.Info("retrying with modified fact gathering settings")
		baseOpts.Forks = 1
		baseOpts.ExtraVars = map[string]string{
			"ansible_facts_gathering_timeout": "60",
			"gather_timeout":                  "60",
		}
		baseOpts.ExtraEnv = map[string]string{
			"ANSIBLE_GATHERING": "explicit",
		}
		return RunPlaybook(ctx, baseOpts)

	case ErrNetworkAdapter:
		log.Info("retrying with network adapter fix")
		baseOpts.ExtraVars = map[string]string{
			"skip_network_adapter_config": "true",
			"bypass_ethernet3_check":      "true",
		}
		return RunPlaybook(ctx, baseOpts)

	case ErrSSMTransfer:
		log.Info("SSM transfer error - fixing ssm-user accounts")
		cleanupSSMSessions(ctx, opts.Env, log)
		fixSSMUsers(ctx, opts.Env, failResult.FailedHosts, log)
		log.Info("waiting for SSM Agent to stabilize", "delay", "30s")
		time.Sleep(30 * time.Second)

		baseOpts.Forks = 1
		baseOpts.ExtraVars = map[string]string{
			"ansible_aws_ssm_retries":     "10",
			"ansible_aws_ssm_retry_delay": "30",
			"ansible_connection_timeout":  "300",
			"ansible_command_timeout":     "300",
			"ansible_aws_ssm_timeout":     "300",
		}
		baseOpts.ExtraEnv = map[string]string{"ANSIBLE_TIMEOUT": "300"}
		return RunPlaybook(ctx, baseOpts)

	case ErrSSMReconnection:
		log.Info("SSM reconnection needed - waiting for systems to reboot")
		cleanupSSMSessions(ctx, opts.Env, log)
		log.Info("waiting for Windows reboot and SSM reconnection", "delay", "120s")
		time.Sleep(120 * time.Second)

		fixSSMUsers(ctx, opts.Env, failResult.FailedHosts, log)
		time.Sleep(10 * time.Second)

		baseOpts.Forks = 1
		baseOpts.ExtraVars = map[string]string{
			"ansible_connection_timeout":      "180",
			"ansible_timeout":                 "180",
			"ansible_facts_gathering_timeout": "60",
		}
		baseOpts.ExtraEnv = map[string]string{"ANSIBLE_TIMEOUT": "180"}
		return RunPlaybook(ctx, baseOpts)

	case ErrPowerShell:
		log.Info("retrying with PowerShell interactive mode fix")
		baseOpts.ExtraVars = map[string]string{
			"ansible_shell_type": "powershell",
			"force_ps_module":    "true",
			"ansible_ps_version": "5.1",
		}
		return RunPlaybook(ctx, baseOpts)

	case ErrSSMUserAccount:
		log.Info("SSM user account issue - recreating as domain account")
		fixSSMUsers(ctx, opts.Env, failResult.FailedHosts, log)
		log.Info("waiting for SSM Agent to stabilize", "delay", "30s")
		time.Sleep(30 * time.Second)

		baseOpts.Forks = 1
		baseOpts.ExtraVars = map[string]string{
			"ansible_connection_timeout": "180",
			"ansible_timeout":            "180",
			"ansible_aws_ssm_timeout":    "300",
		}
		baseOpts.ExtraEnv = map[string]string{"ANSIBLE_TIMEOUT": "180"}
		return RunPlaybook(ctx, baseOpts)

	case ErrMSIInstaller:
		log.Info("MSI installer error - rebooting failed hosts before retry")
		rebootFailedHosts(ctx, opts, log)
		time.Sleep(30 * time.Second)

		baseOpts.Forks = 1
		return RunPlaybook(ctx, baseOpts)

	default:
		log.Info("retrying with general robust settings")
		baseOpts.Forks = 1
		baseOpts.ExtraEnv = map[string]string{
			"ANSIBLE_SSH_RETRIES": "5",
			"ANSIBLE_TIMEOUT":     "120",
		}
		return RunPlaybook(ctx, baseOpts)
	}
}

func buildRetryLimit(userLimit, failedHosts string) string {
	switch {
	case userLimit != "" && failedHosts != "":
		return userLimit + "," + failedHosts
	case userLimit != "":
		return userLimit
	default:
		return failedHosts
	}
}

func cleanupSSMSessions(ctx context.Context, env string, log *slog.Logger) {
	cfg, err := config.Get()
	if err != nil {
		log.Warn("could not get config for SSM cleanup", "error", err)
		return
	}
	inv, err := inventory.Parse(cfg.InventoryPath())
	if err != nil {
		log.Warn("could not parse inventory for SSM cleanup", "error", err)
		return
	}

	client, err := daws.NewClient(ctx, inv.Region())
	if err != nil {
		log.Warn("could not create AWS client for SSM cleanup", "error", err)
		return
	}

	terminated, err := client.CleanupStaleSessions(ctx, inv.InstanceIDs(), 15*time.Minute, false, log)
	if err != nil {
		log.Warn("SSM cleanup error", "error", err)
	}
	if terminated > 0 {
		log.Info("terminated stale SSM sessions", "count", terminated)
		time.Sleep(5 * time.Second)
	}
}

func fixSSMUsers(ctx context.Context, env string, failedHosts []string, log *slog.Logger) {
	if len(failedHosts) == 0 {
		return
	}

	cfg, err := config.Get()
	if err != nil {
		log.Warn("could not get config for ssm-user fix", "error", err)
		return
	}
	inv, err := inventory.Parse(cfg.InventoryPath())
	if err != nil {
		log.Warn("could not parse inventory for ssm-user fix", "error", err)
		return
	}

	client, err := daws.NewClient(ctx, inv.Region())
	if err != nil {
		log.Warn("could not create AWS client for ssm-user fix", "error", err)
		return
	}

	for _, hostName := range failedHosts {
		host := inv.HostByName(hostName)
		if host == nil || host.InstanceID == "" {
			log.Warn("host not found in inventory", "host", hostName)
			continue
		}

		log.Info("fixing ssm-user", "host", hostName, "instance", host.InstanceID)

		if err := client.EnableSSMUserLocal(ctx, host.InstanceID); err != nil {
			log.Info("local enable failed, trying domain account fix", "host", hostName)
			if err := client.FixSSMUserViaDomainAccount(ctx, host.InstanceID); err != nil {
				log.Warn("ssm-user fix failed", "host", hostName, "error", err)
			}
		}
	}
}

func rebootFailedHosts(ctx context.Context, opts RetryOptions, log *slog.Logger) {
	cfg, err := config.Get()
	if err != nil {
		log.Warn("could not get config for reboot", "error", err)
		return
	}
	for _, host := range strings.Split(opts.Limit, ",") {
		if host == "" {
			continue
		}
		log.Info("rebooting host before retry", "host", host)
		args := []string{
			host, "-i", filepath.Join(cfg.ProjectRoot, opts.Env+"-inventory"),
			"-m", "ansible.windows.win_reboot",
			"-a", "reboot_timeout=600 post_reboot_delay=60",
		}
		rebootCmd := execCommand(ctx, "ansible", args...)
		rebootCmd.Dir = cfg.ProjectRoot
		env, envErr := buildEnv(RunOptions{Env: opts.Env}, cfg)
		if envErr != nil {
			log.Warn("could not build env for reboot", "host", host, "error", envErr)
			continue
		}
		rebootCmd.Env = env
		if output, err := rebootCmd.CombinedOutput(); err != nil {
			log.Warn("reboot failed", "host", host, "error", err, "output", string(output))
		}
	}
}

// execCommand is a variable for testability.
var execCommand = exec.CommandContext
