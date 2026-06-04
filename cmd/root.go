package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/voidcubedotgg/hytale-server-runner/internal/config"
	"github.com/voidcubedotgg/hytale-server-runner/internal/logging"
	"go.uber.org/zap"
)

// v is the shared viper instance; exitCode carries the server's exit code out
// through Execute.
var (
	v        = config.New()
	cfgFile  string
	exitCode int
)

var rootCmd = &cobra.Command{
	Use:               "hytale-runner",
	Short:             "Run a Hytale server with OCI-registry-backed state",
	Long:              "hytale-runner loads game server state from an OCI registry, runs the server, then stores the state back.",
	SilenceUsage:      true,
	SilenceErrors:     true,
	PersistentPreRunE: initConfig,
}

// Execute runs the CLI and returns the process exit code (the server's own code
// for `run`, or 1 on a command error).
func Execute() int {
	// Bootstrap a logger so early errors are captured; initConfig rebuilds it at
	// the configured level once flags/env/config are resolved.
	if boot, err := logging.New(config.Default.LogLevel); err == nil {
		zap.ReplaceGlobals(boot)
	}
	defer func() { _ = zap.L().Sync() }()

	if err := rootCmd.Execute(); err != nil {
		zap.S().Errorf("error: %v", err)
		if exitCode == 0 {
			exitCode = 1
		}
	}
	return exitCode
}

func init() {
	pf := rootCmd.PersistentFlags()
	pf.StringVar(&cfgFile, "config", "", "config file (default: ./hytale-runner.yaml or /etc/hytale-runner/)")
	pf.String(config.KeyDataDir, config.Default.DataDir, "mutable state directory")
	pf.String(config.KeyMinMemory, config.Default.MinMemory, "JVM -Xms value")
	pf.String(config.KeyMaxMemory, config.Default.MaxMemory, "JVM -Xmx value")
	pf.String(config.KeyAssetsPath, config.Default.AssetsPath, "path to Assets.zip")
	pf.String(config.KeyServerJarPath, config.Default.ServerJarPath, "path to the server jar")
	pf.String(config.KeyRegistry, config.Default.Registry, "OCI registry host:port (env: HYRUN_REGISTRY)")
	pf.String(config.KeyStateRepo, config.Default.StateRepo, "OCI repository for state")
	pf.String(config.KeyStateTag, config.Default.StateTag, "OCI tag for state")
	pf.String(config.KeyStateArtifact, config.Default.StateArtifact, "OCI artifact type for state")
	pf.Bool(config.KeyPlainHTTP, config.Default.PlainHTTP, "use plain HTTP to the registry")
	pf.String(config.KeyJavaBin, config.Default.JavaBin, "java executable")
	pf.String(config.KeyLogLevel, config.Default.LogLevel, "log level (debug/info/warn/error)")
	pf.StringArray(config.KeyExtraJVMArgs, nil, "extra JVM arg, before -jar (repeatable)")
	pf.StringArray(config.KeyExtraServerArgs, nil, "extra server arg, after the jar (repeatable)")

	for _, key := range []string{
		config.KeyDataDir, config.KeyMinMemory, config.KeyMaxMemory, config.KeyAssetsPath,
		config.KeyServerJarPath, config.KeyRegistry, config.KeyStateRepo, config.KeyStateTag,
		config.KeyStateArtifact, config.KeyPlainHTTP, config.KeyJavaBin, config.KeyLogLevel,
		config.KeyExtraJVMArgs, config.KeyExtraServerArgs,
	} {
		_ = v.BindPFlag(key, pf.Lookup(key))
	}

	rootCmd.AddCommand(runCmd, stateCmd, versionCmd)
}

// initConfig reads the config file (if any) onto the shared viper instance. A
// missing default-search file is fine; an explicit --config that is missing errors.
func initConfig(cmd *cobra.Command, args []string) error {
	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
	} else {
		v.SetConfigName("hytale-runner")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("/etc/hytale-runner")
	}
	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return fmt.Errorf("read config: %w", err)
		}
	}

	cfg, err := config.Load(v)
	if err != nil {
		return err
	}
	logger, err := logging.New(cfg.LogLevel)
	if err != nil {
		return err
	}
	zap.ReplaceGlobals(logger)
	return nil
}
