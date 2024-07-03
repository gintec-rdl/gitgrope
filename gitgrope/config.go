package gitgrope

import (
	"os"
	"path"
	"runtime"
	"strings"
	"time"

	"github.com/google/go-github/v59/github"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

type Config struct {
	RepoSettings `yaml:",inline"`
	Repositories []*Repository `yaml:"repos"`
	LogFile      string        `yaml:"log_file"`
	Shell        string        `yaml:"task_shell"`
	PollTime     TimeSeconds   `yaml:"poll_seconds"`
	HttpTimeout  TimeSeconds   `yaml:"http_timeout"`
	FireOnce     bool          `yaml:"fire_once"` // used for debugging only
}

func (cfg *Config) Apply(logger *logrus.Logger) error {
	cfg.Log = logger

	// initialize directory
	if cfg.ReleaseDirectory == "" {
		homedir, err := os.UserHomeDir()
		if err != nil {
			return errors.Wrap(err, "failed to get user home directory")
		}

		// append with app name
		cfg.ReleaseDirectory = path.Join(homedir, "gitgrope")
	}

	clientFactory := func(token string) *github.Client {
		client := github.NewClient(nil).WithAuthToken(token)
		client.Client().Timeout = cfg.HttpTimeout.Duration
		return client
	}

	// root client
	rootClient := clientFactory(cfg.AccessToken)

	waitSwitch := "-c"
	if runtime.GOOS == "windows" {
		waitSwitch = "/c"
	}

	for _, repo := range cfg.Repositories {
		repo.Log = logger

		// verify glob patterns
		if len(repo.AssetsGlobs) == 0 {
			if !repo.GropeEverything {
				return errors.Errorf("%s: missing assets. set `grope_everything: true` to grope all asses", repo.Name)
			}
		}

		//
		for _, task := range repo.Tasks {
			task.Log = logger
			task.Shell = cfg.Shell
			task.WaitSwitch = waitSwitch
		}

		// extract repo info
		parts := strings.Split(repo.Name, "/")
		if len(parts) != 2 {
			return errors.Errorf("invalid repository name: %s. must be {OWNER}/{REPO}", repo.Name)
		}

		repo.Owner = parts[0]
		repo.Repo = parts[1]

		// inherit root access token
		if repo.AccessToken == "" {
			repo.AccessToken = cfg.AccessToken
			repo.Client = rootClient
		} else {
			// repository might belong to another user/org with a different token
			repo.Client = clientFactory(repo.AccessToken)
		}

		// inherit root release directory
		if repo.ReleaseDirectory == "" {
			repo.ReleaseDirectory = path.Join(cfg.ReleaseDirectory, repo.Owner, repo.Repo)
		}
		err := os.MkdirAll(repo.ReleaseDirectory, os.ModePerm)

		if err != nil {
			return errors.Wrapf(err, "failed to create release directory for repo %s", repo.Name)
		}
	}
	return nil
}

func LoadConfig(filename string) (*Config, error) {
	fd, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer fd.Close()

	var shell string

	if runtime.GOOS == "windows" {
		shell = "cmd.exe"
	} else {
		shell = "/bin/sh"
	}

	config := Config{
		PollTime:     TimeSeconds{Duration: time.Second * 60}, //
		HttpTimeout:  TimeSeconds{Duration: time.Second * 30},
		Repositories: []*Repository{},
		Shell:        shell,
	}

	decoder := yaml.NewDecoder(fd)
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		return nil, err
	}
	return &config, nil
}
