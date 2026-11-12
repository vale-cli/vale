package core

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"

	"github.com/errata-ai/ini"

	"github.com/vale-cli/vale/v3/internal/glob"
	"github.com/vale-cli/vale/v3/internal/system"
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
	ScopeDir  = filepath.Join(ConfigDir, "scopes")
	ModelDir  = filepath.Join(ConfigDir, "models")
	// GrammarDir holds the tree-sitter grammars, one module per language.
	GrammarDir = filepath.Join(ConfigDir, "grammars")
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
	ScopeDir,
	ModelDir,
	GrammarDir,
}

// ConfigVars is a list of all supported environment variables.
var ConfigVars = map[string]string{
	"VALE_CONFIG_PATH":   "Override the default search process by specifying a .vale.ini file.",
	"VALE_STYLES_PATH":   "Specify the location of the default StylesPath.",
	"VALE_MODELS_URL":    "Where `vale sync` downloads language models from: a URL or a directory holding one archive per language.",
	"VALE_GRAMMARS_PATH": "A directory of tree-sitter grammar modules, read when the StylesPath has none.",
	"VALE_GRAMMARS_URL":  "Where `vale sync` downloads grammars from: a URL or a directory holding one archive per grammar.",
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

// nestedConfigNames are checked at each level of the search after
// configNames, so a root-level file wins. See #1212.
var nestedConfigNames = []string{
	filepath.Join(".config", "vale.ini"),
}

// FindConfigAsset tries to locate a Vale-related resource by looking in the
// user-defined StylesPath(s).
func FindConfigAsset(cfg *Config, name, dir string) string {
	return getConfigAsset(cfg.FS, name, cfg.SearchPaths(), []string{dir})
}

// FindAsset tries to locate a Vale-related resource by looking in the
// user-defined StylesPath.
func FindAsset(cfg *Config, path string) string {
	if path == "" {
		return ""
	}

	for _, p := range cfg.SearchPaths() {
		inPath := filepath.Join(p, path)
		if cfg.FS.Exists(inPath) {
			return inPath
		}
	}

	if p := getConfigAsset(cfg.FS, path, cfg.SearchPaths(), ConfigDirs); p != "" {
		return p
	}

	p := system.DeterminePath(cfg.FS, cfg.Flags.Path, path)
	if cfg.FS.Exists(p) {
		return p
	}

	return ""
}

// IgnoreFiles returns a list of all user-defined ignore files.
func IgnoreFiles(fsys system.FS, stylesPath string) ([]string, error) {
	ignore := filepath.Join(stylesPath, IgnoreDir)
	return fsys.GlobRecursive(ignore, "*.txt")
}

// getConfigAsset returns the path to a given asset if it exists.
// Otherwise, it returns an empty string.
func getConfigAsset(fsys system.FS, target string, paths, dirs []string) string {
	for _, p := range paths {
		for _, dir := range dirs {
			path := filepath.Join(p, dir, target)
			if fsys.Exists(path) {
				return path
			}
		}
	}
	return ""
}

// CLIFlags holds the values that are defined at runtime by the user.
//
// For example, `vale --minAlertLevel=error`.
type CLIFlags struct {
	AlertLevel    string
	Built         string
	Glob          string
	InExt         string
	InPath        string
	Output        string
	Path          string
	Sources       string
	Filter        string
	Apply         bool
	Counts        bool
	Coverage      bool
	Local         bool
	NoExit        bool
	NoColor       bool
	PlainProgress bool
	Normalize     bool
	Relative      bool
	Remote        bool
	Simple        bool
	Sorted        bool
	Wrap          bool
	Version       bool
	Help          bool
	IgnoreGlobal  bool

	// FS is the filesystem the resulting Config reads its assets from. nil,
	// the zero value, means system.Default.
	FS system.FS
}

// Config holds the configuration values from both the CLI and `.vale.ini`.
type Config struct {
	// General configuration
	BlockIgnores      map[string][]string          // A list of blocks to ignore
	Checks            []string                     // All checks to load
	Formats           map[string]string            // A map of unknown -> known formats
	Asciidoctor       map[string]string            // A map of asciidoctor attributes
	Sphinx            map[string]string            // The `[sphinx]` section: a project's directives and roles
	FormatToLang      map[string]string            // A map of format to lang ID
	GBaseStyles       []string                     // Global base style
	GChecks           map[string]bool              // Global checks
	IgnoredClasses    []string                     // A list of HTML classes to ignore
	IgnoredScopes     []string                     // A list of HTML tags to ignore
	MinAlertLevel     int                          // Lowest alert level to display
	Vocab             []string                     // The active project
	Grammars          []string                     // Tree-sitter grammars to sync: names, or ALL
	RuleToLevel       map[string]string            // Single-rule level changes
	RuleToParams      map[string]map[string]string // Single-rule scalar overrides
	SBaseStyles       map[string][]string          // Syntax-specific base styles
	SChecks           map[string]map[string]bool   // Syntax-specific checks
	SLevels           map[string]map[string]string // Syntax-specific level changes
	SUnsets           map[string][]string          // Keys a section marked UNSET
	SVocab            map[string][]string          // Vocabularies a section names
	StyleSources      map[string]string            // The file each section's BasedOnStyles comes from
	Vocabularies      map[string]*Vocabulary       // Every vocabulary loaded, by name
	SkippedScopes     []string                     // A list of HTML blocks to ignore
	Stylesheets       map[string]string            // XSLT stylesheet
	TokenIgnores      map[string][]string          // A list of tokens to ignore
	CommentDelimiters map[string][2]string         // Strings to treat as comment delimiters. Indicates the start and end delimiters.
	WordTemplate      string                       // The template used in YAML -> regexp list conversions
	RootINI           string                       // the path to the project's .vale.ini file
	RootDir           string                       // the directory the search found RootINI at
	// FS is the filesystem this Config's assets -- styles, vocabularies,
	// dictionaries, filters, views, templates -- are read from. It defaults
	// to system.Default; a host serving concurrent requests over in-memory
	// files gives each Config one of its own, so no two share mutable state.
	FS          system.FS `json:"-"`
	Paths       []string  // A list of paths to search for styles
	ConfigFiles []string  // A list of configuration files to load

	AcceptedTokens []string `json:"-"` // Project-specific vocabulary (okay)
	RejectedTokens []string `json:"-"` // Project-specific vocabulary (avoid)

	FallbackPath string `json:"-"`

	cache    *sync.Map            // what checks build from this configuration, by key
	SecToPat map[string]glob.Glob `json:"-"`
	// SecToCond holds the condition of a section written `[glob if expr]`.
	SecToCond map[string]*Condition `json:"-"`
	Styles    []string              `json:"-"`
	Views     map[string]*View      `json:"-"`

	// Scopes are the named scopes found under `config/scopes`, by name.
	Scopes       map[string]string `json:"-"`
	scopesLoaded bool

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
	cfg.Sphinx = make(map[string]string)
	cfg.GChecks = make(map[string]bool)
	cfg.MinAlertLevel = 0
	cfg.RuleToLevel = make(map[string]string)
	cfg.RuleToParams = make(map[string]map[string]string)
	cfg.SBaseStyles = make(map[string][]string)
	cfg.SChecks = make(map[string]map[string]bool)
	cfg.SLevels = make(map[string]map[string]string)
	cfg.SUnsets = make(map[string][]string)
	cfg.SVocab = make(map[string][]string)
	cfg.Vocabularies = make(map[string]*Vocabulary)
	cfg.cache = &sync.Map{}
	cfg.SecToPat = make(map[string]glob.Glob)
	cfg.SecToCond = make(map[string]*Condition)
	cfg.Stylesheets = make(map[string]string)
	cfg.TokenIgnores = make(map[string][]string)
	cfg.CommentDelimiters = make(map[string][2]string)
	cfg.FormatToLang = make(map[string]string)
	cfg.Views = make(map[string]*View)
	cfg.Scopes = make(map[string]string)
	cfg.Paths = []string{}
	cfg.ConfigFiles = []string{}

	if flags.FS != nil {
		cfg.FS = flags.FS
	} else {
		cfg.FS = system.Default
	}

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

// setRoot records the config file a run is reading and its project root: the
// file's own directory. The search sets both itself, since a `.config/vale.ini`
// is found a level above its directory.
func (c *Config) setRoot(file string) {
	if abs, err := filepath.Abs(file); err == nil {
		file = abs
	}
	c.RootINI = file
	c.RootDir = filepath.Dir(file)
}

// ConfigFile returns the last configuration file in the list.
//
// The user's project-agnostic file is added first, so this is the project's
// own file when a run has one.
func (c *Config) ConfigFile() string {
	if len(c.ConfigFiles) > 0 {
		return c.ConfigFiles[len(c.ConfigFiles)-1]
	}
	return ""
}

// Root returns the first configuration file in the list.
//
// NOTE: loading records every config file we read -- including the default one
// -- so an empty list means that we found nothing to read.
func (c *Config) Root() (string, error) {
	if len(c.ConfigFiles) > 0 {
		return c.ConfigFiles[0], nil
	}
	return "", errors.New("no .vale.ini file found")
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

// A Vocabulary is what one `config/vocabularies` directory holds.
type Vocabulary struct {
	Accepted []string
	Rejected []string
}

// SectionVocabs names every vocabulary some section uses.
func (c *Config) SectionVocabs() []string {
	var names []string
	for _, sec := range append([]string{"*"}, c.RuleKeys...) {
		for _, name := range c.SVocab[sec] {
			if !StringInSlice(name, names) {
				names = append(names, name)
			}
		}
	}
	return names
}

// Cached returns what build makes for key, building it once per config.
func (c *Config) Cached(key string, build func() any) any {
	if c.cache == nil {
		c.cache = &sync.Map{}
	}
	if v, ok := c.cache.Load(key); ok {
		return v
	}
	v, _ := c.cache.LoadOrStore(key, build())
	return v
}

// AddWordListFile adds vocab terms from a provided file.
func (c *Config) AddWordListFile(name string, accept bool) error {
	fd, err := c.FS.Open(name)
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

	b, err := system.ReadFile(src)
	if err != nil {
		return packages, err
	}
	uCfg, err := ini.Load(b)
	if err != nil {
		return packages, err
	}

	core := uCfg.Section("")
	return core.Key("Packages").Strings(","), nil
}

func pipeConfig(cfg *Config) ([]string, error) {
	var sources []string

	pipeline := filepath.Join(cfg.StylesPath(), ".vale-config")
	if cfg.FS.IsDir(pipeline) && len(cfg.Flags.Sources) == 0 {
		configs, err := cfg.FS.Glob(filepath.Join(pipeline, "*"))
		if err != nil {
			return sources, err
		}

		for _, config := range configs {
			if cfg.FS.IsDir(config) {
				continue
			}
			sources = append(sources, config)
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

// SphinxNames returns one list from the `[sphinx]` section, lowercased.
func (c *Config) SphinxNames(key string) []string {
	out := []string{}
	for _, n := range strings.Split(c.Sphinx[key], ",") {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, strings.ToLower(n))
		}
	}
	return out
}

// DefaultConfigTemplate is the default configuration template written by `vale init`.
const DefaultConfigTemplate = `# Core settings
# StylesPath defines where Vale looks for styles and downloaded packages.
StylesPath = styles

# MinAlertLevel sets the minimum alert severity to display: suggestion, warning, or error.
MinAlertLevel = suggestion

# External packages installed via 'vale sync'.
Packages = Google

# Format-specific configuration
# Applies the Vale and Google styles to Markdown and text files.
[*.{md,txt}]
BasedOnStyles = Vale, Google
`

// InitConfig creates a default .vale.ini configuration file at target and
// scaffolds the styles directory.
// Returns an error if a configuration file already exists at target or
// within target's directory.
func InitConfig(target string) error {
	if target == "" {
		target = ".vale.ini"
	} else if system.IsDir(target) {
		target = filepath.Join(target, ".vale.ini")
	}

	if system.FileExists(target) {
		return NewE100("init", fmt.Errorf("configuration file '%s' already exists", target))
	}

	dir := filepath.Dir(target)
	for _, name := range configNames {
		candidate := filepath.Join(dir, name)
		if system.FileExists(candidate) {
			return NewE100("init", fmt.Errorf("configuration file '%s' already exists", candidate))
		}
	}

	if dir != "" && dir != "." && !system.IsDir(dir) {
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			return NewE100("init", fmt.Errorf("failed to create directory '%s': %w", dir, err))
		}
	}

	if err := os.WriteFile(target, []byte(DefaultConfigTemplate), 0o644); err != nil {
		return NewE100("init", fmt.Errorf("failed to write configuration file '%s': %w", target, err))
	}

	stylesDir := filepath.Join(dir, "styles")
	if err := os.MkdirAll(stylesDir, os.ModePerm); err != nil {
		return NewE100("init", fmt.Errorf("failed to create styles directory '%s': %w", stylesDir, err))
	}

	return nil
}
