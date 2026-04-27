package ansible

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/inventory"
	"github.com/dreadnode/dreadgoad/internal/provider"
)

// RetryOptions configures the retry behavior for a [RunPlaybookWithRetry] call.
// MaxRetries and RetryDelay default to the values from the global [config.Config]
// when left as zero.
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

// RunPlaybookWithRetry runs an Ansible playbook with error-specific retry logic.
// On each failure it classifies the error via [DetectErrorType] and applies a
// targeted recovery strategy (e.g. SSM session cleanup, host reboots) before
// retrying. It returns an error if all attempts are exhausted or the context
// is cancelled.
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

	retryForks := 2 // limit SSM concurrency to avoid session saturation
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
			Forks:       retryForks,
		})

		if result.TimedOut {
			log.Error("playbook timed out (idle timeout)", "playbook", opts.Playbook)
			CleanupSSMSessions(ctx, opts.Env, log)
			retryForks = 1
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
		ExtraVars:   copyVars(opts.ExtraVars),
	}

	switch failResult.ErrorType {
	case ErrFactGathering:
		log.Info("retrying with modified fact gathering settings")
		baseOpts.Forks = 1
		mergeVars(baseOpts.ExtraVars, map[string]string{
			"ansible_facts_gathering_timeout": "60",
			"gather_timeout":                  "60",
		})
		baseOpts.ExtraEnv = map[string]string{
			"ANSIBLE_GATHERING": "explicit",
		}
		return RunPlaybook(ctx, baseOpts)

	case ErrNetworkAdapter:
		log.Info("retrying with network adapter fix")
		mergeVars(baseOpts.ExtraVars, map[string]string{
			"skip_network_adapter_config": "true",
			"bypass_ethernet3_check":      "true",
		})
		return RunPlaybook(ctx, baseOpts)

	case ErrSSMTransfer:
		log.Info("SSM transfer error - fixing ssm-user accounts")
		CleanupSSMSessions(ctx, opts.Env, log)
		fixSSMUsers(ctx, opts.Env, failResult.FailedHosts, log)
		log.Info("waiting for SSM Agent to stabilize", "delay", "30s")
		time.Sleep(30 * time.Second)

		baseOpts.Forks = 1
		mergeVars(baseOpts.ExtraVars, map[string]string{
			"ansible_aws_ssm_retries":     "10",
			"ansible_aws_ssm_retry_delay": "30",
			"ansible_connection_timeout":  "300",
			"ansible_command_timeout":     "300",
			"ansible_aws_ssm_timeout":     "300",
		})
		baseOpts.ExtraEnv = map[string]string{"ANSIBLE_TIMEOUT": "300"}
		return RunPlaybook(ctx, baseOpts)

	case ErrSSMReconnection:
		log.Info("SSM reconnection needed - waiting for systems to reboot")
		CleanupSSMSessions(ctx, opts.Env, log)
		log.Info("waiting for Windows reboot and SSM reconnection", "delay", "120s")
		time.Sleep(120 * time.Second)

		fixSSMUsers(ctx, opts.Env, failResult.FailedHosts, log)
		time.Sleep(10 * time.Second)

		baseOpts.Forks = 1
		mergeVars(baseOpts.ExtraVars, map[string]string{
			"ansible_connection_timeout":      "180",
			"ansible_timeout":                 "180",
			"ansible_facts_gathering_timeout": "60",
		})
		baseOpts.ExtraEnv = map[string]string{"ANSIBLE_TIMEOUT": "180"}
		return RunPlaybook(ctx, baseOpts)

	case ErrPowerShell:
		log.Info("retrying with PowerShell interactive mode fix")
		mergeVars(baseOpts.ExtraVars, map[string]string{
			"ansible_shell_type": "powershell",
			"force_ps_module":    "true",
			"ansible_ps_version": "5.1",
		})
		return RunPlaybook(ctx, baseOpts)

	case ErrSSMUserAccount:
		log.Info("SSM user account issue - recreating as domain account")
		fixSSMUsers(ctx, opts.Env, failResult.FailedHosts, log)
		log.Info("waiting for SSM Agent to stabilize", "delay", "30s")
		time.Sleep(30 * time.Second)

		baseOpts.Forks = 1
		mergeVars(baseOpts.ExtraVars, map[string]string{
			"ansible_connection_timeout": "180",
			"ansible_timeout":            "180",
			"ansible_aws_ssm_timeout":    "300",
		})
		baseOpts.ExtraEnv = map[string]string{"ANSIBLE_TIMEOUT": "180"}
		return RunPlaybook(ctx, baseOpts)

	case ErrMSIInstaller:
		log.Info("MSI installer error - rebooting failed hosts before retry")
		rebootFailedHosts(ctx, opts, log)
		time.Sleep(30 * time.Second)

		baseOpts.Forks = 1
		return RunPlaybook(ctx, baseOpts)

	case ErrWUACOM:
		log.Info("WUA COM corruption - rebooting to clear pending registry deletions")
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

// CleanupSSMSessions terminates stale SSM sessions to prevent connection
// saturation. It resolves the provider from config, checks if it supports
// session management, and cleans up sessions older than 15 minutes.
// This is a no-op for non-SSM inventories (e.g. Ludus, Proxmox).
func CleanupSSMSessions(ctx context.Context, env string, log *slog.Logger) {
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

	if !inv.IsSSM() {
		return
	}

	prov, err := cfg.NewProvider(ctx)
	if err != nil {
		log.Warn("could not create provider for SSM cleanup", "error", err)
		return
	}

	sm, ok := prov.(provider.SessionManager)
	if !ok {
		return
	}

	terminated, err := sm.CleanupStaleSessions(ctx, inv.InstanceIDs(), 15*time.Minute, false)
	if err != nil {
		log.Warn("skipping stale SSM session cleanup", "error", err)
		return
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

	if !inv.IsSSM() {
		return
	}

	prov, err := cfg.NewProvider(ctx)
	if err != nil {
		log.Warn("could not create provider for ssm-user fix", "error", err)
		return
	}

	ssmRecovery, ok := prov.(provider.SSMRecovery)
	if !ok {
		return
	}

	for _, hostName := range failedHosts {
		host := inv.HostByName(hostName)
		if host == nil || host.InstanceID == "" {
			log.Warn("host not found in inventory", "host", hostName)
			continue
		}

		log.Info("fixing ssm-user", "host", hostName, "instance", host.InstanceID)

		if err := ssmRecovery.EnableSSMUserLocal(ctx, host.InstanceID); err != nil {
			log.Info("local enable failed, trying domain account fix", "host", hostName)
			// Brief pause to avoid SSM SendCommand throttling on the same instance.
			time.Sleep(5 * time.Second)
			if err := ssmRecovery.FixSSMUserViaDomainAccount(ctx, host.InstanceID); err != nil {
				log.Warn("ssm-user fix failed", "host", hostName, "error", err)
			}
			// FixSSMUserViaDomainAccount already restarts SSM Agent
			continue
		}

		// EnableSSMUserLocal doesn't restart the SSM Agent, but a restart is
		// needed to refresh S3 credentials that cause 403 transfer errors.
		// Brief pause to avoid SSM SendCommand throttling on the same instance.
		time.Sleep(5 * time.Second)
		log.Info("restarting SSM Agent to refresh credentials", "host", hostName)
		if err := ssmRecovery.RestartSSMAgent(ctx, host.InstanceID); err != nil {
			log.Warn("SSM Agent restart failed", "host", hostName, "error", err)
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

var execCommand = exec.CommandContext

// copyVars returns a shallow copy of a vars map (nil-safe).
func copyVars(src map[string]string) map[string]string {
	if src == nil {
		return make(map[string]string)
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// mergeVars adds entries from src into dst without overwriting existing keys.
// This preserves connection-level vars (like SOCKS proxy settings) while
// allowing retry strategies to add their own vars.
func mergeVars(dst, src map[string]string) {
	for k, v := range src {
		if _, exists := dst[k]; !exists {
			dst[k] = v
		}
	}
}
