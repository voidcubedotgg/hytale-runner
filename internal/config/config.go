package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Configuration keys. Shared by the viper bindings here and the cobra flags in
// the cmd package so a name only ever lives in one place.
const (
	KeyDataDir         = "data-dir"
	KeyMinMemory       = "min-memory"
	KeyMaxMemory       = "max-memory"
	KeyAssetsPath      = "assets-path"
	KeyServerJarPath   = "server-jar-path"
	KeyRegistry        = "registry"
	KeyRegistryUser    = "registry-user"
	KeyRegistryPass    = "registry-pass"
	KeyStateRepo       = "state-repo"
	KeyStateTag        = "state-tag"
	KeyStateArtifact   = "state-artifact"
	KeyPlainHTTP       = "plain-http"
	KeyJavaBin         = "java-bin"
	KeyLogLevel        = "log-level"
	KeyExtraJVMArgs    = "extra-jvm-args"
	KeyExtraServerArgs = "extra-server-args"
)

// EnvPrefix namespaces environment variables, e.g. KeyMaxMemory -> HYRUN_MAX_MEMORY.
const EnvPrefix = "HYRUN"

// Config holds every runtime setting. Values flow in via flags > env > config
// file > defaults, resolved by viper.
type Config struct {
	DataDir         string   `mapstructure:"data-dir"`
	MinMemory       string   `mapstructure:"min-memory"`
	MaxMemory       string   `mapstructure:"max-memory"`
	AssetsPath      string   `mapstructure:"assets-path"`
	ServerJarPath   string   `mapstructure:"server-jar-path"`
	Registry        string   `mapstructure:"registry"`
	RegistryUser    string   `mapstructure:"registry-user"`
	RegistryPass    string   `mapstructure:"registry-pass"`
	StateRepo       string   `mapstructure:"state-repo"`
	StateTag        string   `mapstructure:"state-tag"`
	StateArtifact   string   `mapstructure:"state-artifact"`
	PlainHTTP       bool     `mapstructure:"plain-http"`
	JavaBin         string   `mapstructure:"java-bin"`
	LogLevel        string   `mapstructure:"log-level"`
	ExtraJVMArgs    []string `mapstructure:"extra-jvm-args"`
	ExtraServerArgs []string `mapstructure:"extra-server-args"`
}

// Default is the single source of truth for default values, reused both as the
// viper defaults and as the cobra flag defaults.
var Default = Config{
	DataDir:       "/data",
	MinMemory:     "6G",
	MaxMemory:     "6G",
	AssetsPath:    "/hytale/Assets.zip",
	ServerJarPath: "/hytale/HytaleServer.jar",
	Registry:      "localhost:5001",
	StateRepo:     "hytale/state",
	StateTag:      "latest",
	StateArtifact: "application/vnd.hytale.server.state",
	PlainHTTP:     true,
	JavaBin:       "java",
	LogLevel:      "info",
}

// New returns a viper instance wired with the env conventions and defaults
// shared by every command. Callers bind flags and read a config file onto it,
// then call Load.
func New() *viper.Viper {
	v := viper.New()
	v.SetEnvPrefix(EnvPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.AutomaticEnv()

	v.SetDefault(KeyDataDir, Default.DataDir)
	v.SetDefault(KeyMinMemory, Default.MinMemory)
	v.SetDefault(KeyMaxMemory, Default.MaxMemory)
	v.SetDefault(KeyAssetsPath, Default.AssetsPath)
	v.SetDefault(KeyServerJarPath, Default.ServerJarPath)
	v.SetDefault(KeyRegistry, Default.Registry)
	v.SetDefault(KeyStateRepo, Default.StateRepo)
	v.SetDefault(KeyStateTag, Default.StateTag)
	v.SetDefault(KeyStateArtifact, Default.StateArtifact)
	v.SetDefault(KeyPlainHTTP, Default.PlainHTTP)
	v.SetDefault(KeyJavaBin, Default.JavaBin)
	v.SetDefault(KeyLogLevel, Default.LogLevel)
	return v
}

// Load materializes a Config from the viper instance.
func Load(v *viper.Viper) (Config, error) {
	var c Config
	if err := v.Unmarshal(&c); err != nil {
		return Config{}, fmt.Errorf("unmarshal config: %w", err)
	}
	return c, nil
}
