package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const (
	defaultMsBetweenPoll         = 30000
	defaultMaxConcurrentIndexers = 2
	defaultPushEnabled           = false
	defaultPollEnabled           = true
	defaultTitle                 = "Hound"
	defaultVcs                   = "git"
	defaultBaseUrl               = "{url}/blob/{rev}/{path}{anchor}"
	defaultAnchor                = "#L{line}"
	defaultHealthCheckURI        = "/healthz"
	defaultResultLimit           = 5000
)

type UrlPattern struct {
	BaseUrl string `json:"base-url"`
	Anchor  string `json:"anchor"`
}

type Repo struct {
	Url                string         `json:"url"`
	DisplayName        string         `json:"display-name"`
	MsBetweenPolls     int            `json:"ms-between-poll"`
	Vcs                string         `json:"vcs"`
	VcsConfigMessage   *SecretMessage `json:"vcs-config"`
	UrlPattern         *UrlPattern    `json:"url-pattern"`
	ExcludeDotFiles    bool           `json:"exclude-dot-files"`
	EnablePollUpdates  *bool          `json:"enable-poll-updates"`
	EnablePushUpdates  *bool          `json:"enable-push-updates"`
	AutoGeneratedFiles []string       `json:"auto-generated-files"`
}

// Used for interpreting the config value for fields that use *bool. If a value
// is present, that value is returned. Otherwise, the default is returned.
func optionToBool(val *bool, def bool) bool {
	if val == nil {
		return def
	}
	return *val
}

// Are polling based updates enabled on this repo?
func (r *Repo) PollUpdatesEnabled() bool {
	return optionToBool(r.EnablePollUpdates, defaultPollEnabled)
}

// Are push based updates enabled on this repo?
func (r *Repo) PushUpdatesEnabled() bool {
	return optionToBool(r.EnablePushUpdates, defaultPushEnabled)
}

type GitLabInstance struct {
	Url      string `json:"url"`
	TokenENV string `json:"token_env"`
}

type Config struct {
	DbPath                string                    `json:"dbpath"`
	Title                 string                    `json:"title"`
	Repos                 map[string]*Repo          `json:"repos"`
	MaxConcurrentIndexers int                       `json:"max-concurrent-indexers"`
	HealthCheckURI        string                    `json:"health-check-uri"`
	VCSConfigMessages     map[string]*SecretMessage `json:"vcs-config"`
	ResultLimit           int                       `json:"result-limit"`
	GitLabInstance        *GitLabInstance           `json:"gitlab_instance"`
}

// SecretMessage is just like json.RawMessage but it will not
// marshal its value as JSON. This is to ensure that vcs-config
// is not marshalled into JSON and send to the UI.
type SecretMessage []byte

// This always marshals to an empty object.
func (s *SecretMessage) MarshalJSON() ([]byte, error) {
	return []byte("{}"), nil
}

// See http://golang.org/pkg/encoding/json/#RawMessage.UnmarshalJSON
func (s *SecretMessage) UnmarshalJSON(b []byte) error {
	if b == nil {
		return errors.New("SecretMessage: UnmarshalJSON on nil pointer")
	}
	*s = append((*s)[0:0], b...)
	return nil
}

// Get the JSON encode vcs-config for this repo. This returns nil if
// the repo doesn't declare a vcs-config.
func (r *Repo) VcsConfig() []byte {
	if r.VcsConfigMessage == nil {
		return nil
	}
	return *r.VcsConfigMessage
}

// Populate missing config values with default values.
func initRepo(r *Repo) {
	if r.MsBetweenPolls == 0 {
		r.MsBetweenPolls = defaultMsBetweenPoll
	}

	if r.Vcs == "" {
		r.Vcs = defaultVcs
	}

	if r.UrlPattern == nil {
		r.UrlPattern = &UrlPattern{
			BaseUrl: defaultBaseUrl,
			Anchor:  defaultAnchor,
		}
	} else {
		if r.UrlPattern.BaseUrl == "" {
			r.UrlPattern.BaseUrl = defaultBaseUrl
		}

		if r.UrlPattern.Anchor == "" {
			r.UrlPattern.Anchor = defaultAnchor
		}
	}
}

// Populate missing config values with default values and
// merge global VCS configs into repo level configs.
func initConfig(c *Config) error {
	if c.MaxConcurrentIndexers == 0 {
		c.MaxConcurrentIndexers = defaultMaxConcurrentIndexers
	}

	if c.HealthCheckURI == "" {
		c.HealthCheckURI = defaultHealthCheckURI
	}

	if c.ResultLimit == 0 {
		c.ResultLimit = defaultResultLimit
	}

	return mergeVCSConfigs(c)
}

func mergeVCSConfigs(cfg *Config) error {
	globalConfigLen := len(cfg.VCSConfigMessages)
	if globalConfigLen == 0 {
		return nil
	}

	globalConfigVals := make(map[string]map[string]interface{}, globalConfigLen)
	for vcs, configBytes := range cfg.VCSConfigMessages {
		var configVals map[string]interface{}
		if err := json.Unmarshal(*configBytes, &configVals); err != nil {
			return err
		}

		globalConfigVals[vcs] = configVals
	}

	for _, repo := range cfg.Repos {
		var globalVals map[string]interface{}
		globalVals, valsExist := globalConfigVals[repo.Vcs]
		if !valsExist {
			continue
		}

		repoBytes := repo.VcsConfig()
		var repoVals map[string]interface{}
		if len(repoBytes) == 0 {
			repoVals = make(map[string]interface{}, len(globalVals))
		} else if err := json.Unmarshal(repoBytes, &repoVals); err != nil {
			return err
		}

		for name, val := range globalVals {
			if _, ok := repoVals[name]; !ok {
				repoVals[name] = val
			}
		}

		repoBytes, err := json.Marshal(&repoVals)
		if err != nil {
			return err
		}

		repoMessage := SecretMessage(repoBytes)
		repo.VcsConfigMessage = &repoMessage
	}

	return nil
}

func (c *Config) LoadFromFile(filename string) error {
	r, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer r.Close()

	if err := json.NewDecoder(r).Decode(c); err != nil {
		return err
	}

	if c.Title == "" {
		c.Title = defaultTitle
	}

	if !filepath.IsAbs(c.DbPath) {
		path, err := filepath.Abs(
			filepath.Join(filepath.Dir(filename), c.DbPath))
		if err != nil {
			return err
		}
		c.DbPath = path
	}

	for _, repo := range c.Repos {
		initRepo(repo)
	}

	return initConfig(c)
}

func (c *Config) ToJsonString() (string, error) {
	b, err := json.Marshal(c.Repos)
	if err != nil {
		return "", err
	}

	return string(b), nil
}
