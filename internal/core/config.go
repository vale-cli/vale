package core

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/adrg/xdg"
	"github.com/bmatcuk/doublestar/v4"
	"github.com/errata-ai/ini"

	"github.com/errata-ai/vale/v3/internal/glob"
	"github.com/errata-ai/vale/v3/internal/system"
)

var (
	// ConfigDir is the default location for Vale's configuration files.
	//
	// This was introduced in v3.0.0 as a means of standardizing the location
	// of Vale's configuration files.
	//
	// This directory is relative to the user's specified `StylesPath`, which
	// can be set via the `--config` flag, the `VALE_CONFIG_PATH` environment
	// variable, or the default search process.
	//
	// NOTE: The config pipeline is stored in the top-level `.vale-config`
	// directory. See `cmd/vale/sync.go`.
	ConfigDir = "config"

	// PipeDir is the default location for Vale's configuration pipeline.
	PipeDir = ".vale-config"

	VocabDir  = filepath.Join(ConfigDir, "vocabularies")
	DictDir   = filepath.Join(ConfigDir, "dictionaries")
	TmplDir   = filepath.Join(ConfigDir, "templates")
	IgnoreDir = filepath.Join(ConfigDir, "ignore")
	ActionDir = filepath.Join(ConfigDir, "actions")
	FilterDir = filepath.Join(ConfigDir, "filters")
	ScriptDir = filepath.Join(ConfigDir, "scripts")
	ViewDir   = filepath.Join(ConfigDir, "views")
)

// ConfigDirs is a list of all directories that contain user-defined, non-style
// configuration files.
var ConfigDirs = []string{
	VocabDir,
	DictDir,
	TmplDir,
	IgnoreDir,
	ActionDir,
	ScriptDir,
	FilterDir,
	ViewDir,
}

// ConfigVars is a list of all supported environment variables.
var ConfigVars = map[string]string{
	"VALE_CONFIG_PATH": "Override the default search process by specifying a .vale.ini file.",
	"VALE_STYLES_PATH": "Specify the location of the default StylesPath.",
}

// ConfigNames is a list of all possible configuration file names.
//
// NOTE: This is leftover from the early days of Vale; we have now standardized
// on `.vale.ini` for documentation purposes.
var configNames = []string{
	".vale",
	"_vale",
	"vale.ini",
	".vale.ini",
	"_vale.ini",
}

// FindConfigAsset tries to locate a Vale-related resource by looking in the
// user-defined StylesPath(s).
func FindConfigAsset(cfg *Config, name, dir string) string {
	return getConfigAsset(name, cfg.SearchPaths(), []string{dir})
}

// FindAsset tries to locate a Vale-related resource by looking in the
// user-defined StylesPath.
func FindAsset(cfg *Config, path string) string {
	if path == "" {
		return ""
	}

	for _, p := range cfg.SearchPaths() {
		inPath := filepath.Join(p, path)
		if system.FileExists(inPath) {
			return inPath
		}
	}

	if p := getConfigAsset(path, cfg.SearchPaths(), ConfigDirs); p != "" {
		return p
	}

	p := system.DeterminePath(cfg.Flags.Path, path)
	if system.FileExists(p) {
		return p
	}

	return ""
}

// IgnoreFiles returns a list of all user-defined ignore files.
func IgnoreFiles(stylesPath string) ([]string, error) {
	ignore := filepath.Join(stylesPath, IgnoreDir)
	return doublestar.FilepathGlob(filepath.Join(ignore, "**", "*.txt"))
}

// getConfigAsset returns the path to a given asset if it exists.
// Otherwise, it returns an empty string.
func getConfigAsset(target string, paths, dirs []string) string {
	for _, p := range paths {
		for _, dir := range dirs {
			path := filepath.Join(p, dir, target)
			if system.FileExists(path) {
				return path
			}
		}
	}
	return ""
}

// DefaultConfig returns the path to the default configuration file.
//
// We don't create this file automatically because there's no actual notion of
// a "default" configuration -- it's just a file loation.
//
// NOTE: if this file does not exist *and* the user has not specified a
// project-specific configuration file, Vale raises an error.
func DefaultConfig() (string, error) {
	root, err := xdg.ConfigFile("vale/.vale.ini")
	if err != nil {
		return "", fmt.Errorf("failed to find default config: %w", err)
	}
	return root, nil
}

// DefaultStylesPath returns the path to the default styles directory.
//
// NOTE: the default styles directory is only used if neither the
// project-specific nor the global configuration file specify a `StylesPath`.
func DefaultStylesPath() (string, error) {
	if fromEnv, hasEnv := os.LookupEnv("VALE_STYLES_PATH"); hasEnv {
		return fromEnv, nil
	}

	styles, err := xdg.DataFile("vale/styles/config.yml")
	if err != nil {
		return "", fmt.Errorf("failed to find default styles: %w", err)
	}

	return filepath.Dir(styles), nil
}

// CLIFlags holds the values that are defined at runtime by the user.
//
// For example, `vale --minAlertLevel=error`.
type CLIFlags struct {
	AlertLevel   string
	Built        string
	Glob         string
	InExt        string
	InPath       string
	Output       string
	Path         string
	Sources      string
	Filter       string
	Local        bool
	NoExit       bool
	Normalize    bool
	Relative     bool
	Remote       bool
	Simple       bool
	Sorted       bool
	Wrap         bool
	Version      bool
	Help         bool
	IgnoreGlobal bool
}

// Config holds the configuration values from both the CLI and `.vale.ini`.
type Config struct {
	// General configuration
	BlockIgnores      map[string][]string        // A list of blocks to ignore
	Checks            []string                   // All checks to load
	Formats           map[string]string          // A map of unknown -> known formats
	Asciidoctor       map[string]string          // A map of asciidoctor attributes
	FormatToLang      map[string]string          // A map of format to lang ID
	GBaseStyles       []string                   // Global base style
	GChecks           map[string]bool            // Global checks
	IgnoredClasses    []string                   // A list of HTML classes to ignore
	IgnoredScopes     []string                   // A list of HTML tags to ignore
	MinAlertLevel     int                        // Lowest alert level to display
	Vocab             []string                   // The active project
	RuleToLevel       map[string]string          // Single-rule level changes
	SBaseStyles       map[string][]string        // Syntax-specific base styles
	SChecks           map[string]map[string]bool // Syntax-specific checks
	SkippedScopes     []string                   // A list of HTML blocks to ignore
	Stylesheets       map[string]string          // XSLT stylesheet
	TokenIgnores      map[string][]string        // A list of tokens to ignore
	CommentDelimiters map[string][2]string       // Strings to treat as comment delimiters. Indicates the start and end delimiters.
	WordTemplate      string                     // The template used in YAML -> regexp list conversions
	RootINI           string                     // the path to the project's .vale.ini file
	Paths             []string                   // A list of paths to search for styles
	ConfigFiles       []string                   // A list of configuration files to load

	AcceptedTokens []string `json:"-"` // Project-specific vocabulary (okay)
	RejectedTokens []string `json:"-"` // Project-specific vocabulary (avoid)

	FallbackPath string               `json:"-"`
	SecToPat     map[string]glob.Glob `json:"-"`
	Styles       []string             `json:"-"`
	Views        map[string]*View     `json:"-"`

	NLPEndpoint string // An external API to call for NLP-related work.

	// Command-line configuration
	Flags *CLIFlags `json:"-"`

	StyleKeys []string `json:"-"`
	RuleKeys  []string `json:"-"`
}

// NewConfig initializes a Config with its default values.
func NewConfig(flags *CLIFlags) (*Config, error) {
	var cfg Config

	cfg.BlockIgnores = make(map[string][]string)
	cfg.Flags = flags
	cfg.Formats = make(map[string]string)
	cfg.Asciidoctor = make(map[string]string)
	cfg.GChecks = make(map[string]bool)
	cfg.MinAlertLevel = 0
	cfg.RuleToLevel = make(map[string]string)
	cfg.SBaseStyles = make(map[string][]string)
	cfg.SChecks = make(map[string]map[string]bool)
	cfg.SecToPat = make(map[string]glob.Glob)
	cfg.Stylesheets = make(map[string]string)
	cfg.TokenIgnores = make(map[string][]string)
	cfg.CommentDelimiters = make(map[string][2]string)
	cfg.FormatToLang = make(map[string]string)
	cfg.Views = make(map[string]*View)
	cfg.Paths = []string{}
	cfg.ConfigFiles = []string{}

	found, _ := DefaultStylesPath()
	if !flags.IgnoreGlobal && system.IsDir(found) {
		cfg.AddStylesPath(found)
	}

	return &cfg, nil
}

// AddConfigFile adds a new configuration file to the current list.
func (c *Config) AddConfigFile(name string) {
	if !StringInSlice(name, c.ConfigFiles) {
		c.ConfigFiles = append(c.ConfigFiles, name)
	}
}

// AddStylesPath adds a new path to the current list.
func (c *Config) AddStylesPath(path string) {
	if !StringInSlice(path, c.Paths) && path != "" {
		c.Paths = append(c.Paths, path)
	}
}

// ConfigFile returns the last configuration file in the list.
//
// This represents the user's project-agnostic configuration file -- i.e., the
// last one that was added.
func (c *Config) ConfigFile() string {
	if len(c.ConfigFiles) > 0 {
		return c.ConfigFiles[len(c.ConfigFiles)-1]
	}
	return ""
}

// Root returns the first configuration file in the list.
func (c *Config) Root() (string, error) {
	if len(c.ConfigFiles) > 0 {
		return c.ConfigFiles[0], nil
	}

	root, err := DefaultConfig()
	if err != nil {
		return "", err
	}

	if !system.FileExists(root) {
		return "", fmt.Errorf("no .vale.ini file found")
	}

	return root, nil
}

// GetStylesPath returns the last path in the list.
//
// This represents the user's project-specific styles directory -- i.e., the
// last one that was added.
func (c *Config) StylesPath() string {
	if len(c.Paths) > 0 {
		return c.Paths[len(c.Paths)-1]
	}
	return ""
}

func (c *Config) SearchPaths() []string {
	if len(c.Paths) == 0 {
		// This represents the 'no value set' case.
		return []string{""}
	}
	return c.Paths
}

// AddWordListFile adds vocab terms from a provided file.
func (c *Config) AddWordListFile(name string, accept bool) error {
	fd, err := os.Open(name)
	if err != nil {
		return err
	}
	defer fd.Close()
	return c.addWordList(fd, accept)
}

func (c *Config) addWordList(r io.Reader, accept bool) error {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		word := strings.TrimSpace(scanner.Text())
		if len(word) == 0 || strings.HasPrefix(word, "# ") { //nolint:gocritic
			continue
		} else if accept {
			c.AcceptedTokens = append(c.AcceptedTokens, word)
		} else {
			c.RejectedTokens = append(c.RejectedTokens, word)
		}
	}
	return scanner.Err()
}

func (c *Config) String() string {
	b, _ := json.MarshalIndent(c, "", "  ")
	return string(b)
}

// Get the user-defined packages from a `.vale.ini` file.
func GetPackages(src string) ([]string, error) {
	packages := []string{}

	uCfg, err := ini.Load(src)
	if err != nil {
		return packages, err
	}

	core := uCfg.Section("")
	return core.Key("Packages").Strings(","), nil
}

func pipeConfig(cfg *Config) ([]string, error) {
	var sources []string

	pipeline := filepath.Join(cfg.StylesPath(), ".vale-config")
	if system.IsDir(pipeline) && len(cfg.Flags.Sources) == 0 {
		configs, err := os.ReadDir(pipeline)
		if err != nil {
			return sources, err
		}

		for _, config := range configs {
			if config.IsDir() {
				continue
			}
			sources = append(sources, filepath.Join(pipeline, config.Name()))
		}
		sources = append(sources, cfg.ConfigFiles...)
	}

	return sources, nil
}

// MockLoad returns the would-be configuration after loading two files, one
// from the project and one from the user's local directory.
func MockLoad(project, local string, cfg *Config) error {
	uCfg, err := ini.LoadSources(ini.LoadOptions{
		AllowShadows:             true,
		SpaceBeforeInlineComment: true}, project, local)

	if err != nil {
		return err
	}

	_, err = processConfig(uCfg, cfg, true)
	return err
}
