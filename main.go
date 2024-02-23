package main

import (
	"context"
	"flag"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/gobwas/glob"
	"github.com/google/go-github/v59/github"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
	"gopkg.in/yaml.v3"
)

type AssetGlob struct {
	Globber glob.Glob
	Pattern string
}

type TimeSeconds struct {
	time.Duration
}

func (ts *TimeSeconds) UnmarshalYAML(value *yaml.Node) error {
	var intValue int = 0
	err := value.Decode(&intValue)
	if err != nil {
		return err
	}
	ts.Duration = time.Second * time.Duration(intValue)
	return nil
}

func (ag *AssetGlob) UnmarshalYAML(value *yaml.Node) error {
	err := value.Decode(&ag.Pattern)
	if err != nil {
		return err
	}
	ag.Globber, err = glob.Compile(ag.Pattern)
	if err != nil {
		return errors.Wrapf(err, "invalid asset glob pattern: %s", ag.Pattern)
	}
	return nil
}

type RepoSettings struct {
	ReleaseDirectory string `yaml:"release_dir"`
	AccessToken      string `yaml:"access_token"`
}

type Repository struct {
	RepoSettings    `yaml:",inline"`
	Name            string `yaml:"name"` // owner/repo
	Owner           string
	Repo            string
	AssetsGlobs     []AssetGlob `yaml:"assets"`
	Tasks           []*Task     `yaml:"tasks"`
	GropeEverything bool        `yaml:"grope_everything"`
	Client          *github.Client
}

func (r *Repository) GropeAsset(ctx context.Context, asset *github.ReleaseAsset, dst string) error {
	rc, _, err := r.Client.Repositories.DownloadReleaseAsset(ctx, r.Owner, r.Repo, asset.GetID(), http.DefaultClient)
	if err != nil {
		return errors.Wrapf(err, "%s.%s: asset groping failed", r.Name, asset.GetName())
	}
	defer rc.Close()
	dstfd, err := os.Create(dst)
	if err != nil {
		return errors.Wrapf(err, "%s.%s: asset groping failed", r.Name, asset.GetName())
	}
	defer dstfd.Close()

	_, err = io.Copy(dstfd, rc)
	if err != nil {
		return errors.Wrapf(err, "%s.%s: asset groping failed", r.Name, asset.GetName())
	}

	return nil
}

func (r *Repository) FeelAndGrope(ctx context.Context) {
	release, _, err := r.Client.Repositories.GetLatestRelease(ctx, r.Owner, r.Repo)
	if err != nil {
		log.WithError(err).Errorf("%s: error checking for latest releases", r.Name)
		return
	}

	if release.GetPrerelease() || release.GetDraft() {
		log.Infof("%s.%s: skipped draft/pre-release", r.Name, release.GetTagName())
		return
	}

	// check if .release file exists
	releaseFile := path.Join(r.ReleaseDirectory, release.GetTagName()+".release")
	_, err = os.Stat(releaseFile)

	// base directory
	basedir := path.Join(r.ReleaseDirectory, release.GetTagName())

	if err := os.MkdirAll(basedir, os.ModePerm); err != nil {
		log.WithError(err).Errorf("%s.%s: error creating download destination", r.Name, release.GetTagName())
		return
	}

	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			log.Infof("%s.%s: not found locally.", r.Name, release.GetTagName())

			assets := make([]*github.ReleaseAsset, 0)
			if r.GropeEverything {
				log.Infof("%s.%s: will grope everything", r.Name, release.GetTagName())
				assets = append(assets, release.Assets...)
			} else {
				for _, assetGlob := range r.AssetsGlobs {
					for _, releaseAsset := range release.Assets {
						if assetGlob.Globber.Match(releaseAsset.GetName()) {
							assets = append(assets, releaseAsset)
						}
					}
				}
			}

			//
			log.Infof("%s.%s: groping %d assets...", r.Name, release.GetTagName(), len(assets))
			successfullyGroped := true

			for _, asset := range assets {
				assetDst := path.Join(basedir, asset.GetName())
				log.Infof("%s.%s: groping %s to %s", r.Name, release.GetTagName(), asset.GetName(), assetDst)

				err := r.GropeAsset(ctx, asset, assetDst)
				if err != nil {
					log.WithError(err).Error()
					successfullyGroped = false
				}
			}

			if successfullyGroped {
				log.Infof("%s.%s: running tasks", r.Name, release.GetTagName())
				assetNames := []string{}
				for _, asset := range assets {
					assetNames = append(assetNames, asset.GetName())
				}
				assetNameList := strings.Join(assetNames, ";")
				for _, task := range r.Tasks {
					if !task.ExecuteFor(r, release, basedir, assetNameList) {
						successfullyGroped = false
						break
					}
				}
				if successfullyGroped {
					// register release
					log.Infof("%s.%s: save release info", r.Name, release.GetTagName())

					if err := os.WriteFile(releaseFile, []byte{}, os.ModePerm); err != nil {
						log.WithError(err).Errorf("%s.%s: error saving release info", r.Name, release.GetTagName())
					}
				}
			}
		} else {
			log.WithError(err).Errorf("%s: error checking for local release", r.Name)
		}
		return
	} else {
		log.Infof("%s.%s exists locally", r.Name, release.GetTagName())
	}
}

type Config struct {
	RepoSettings `yaml:",inline"`
	Repositories []*Repository `yaml:"repos"`
	LogFile      string        `yaml:"log_file"`
	Shell        string        `yaml:"task_shell"`
	PollTime     TimeSeconds   `yaml:"poll_seconds"`
	HttpTimeout  TimeSeconds   `yaml:"http_timeout"`
}

func (cfg *Config) Apply() error {

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
		client := github.NewClient(nil).WithAuthToken(cfg.AccessToken)
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

		// verify glob patterns
		if len(repo.AssetsGlobs) == 0 {
			if !repo.GropeEverything {
				return errors.Errorf("%s: missing assets. set `grope_everything: true` to grope all asses", repo.Name)
			}
		}

		//
		for _, task := range repo.Tasks {
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

var (
	configFile string
	log        = logrus.New()
)

func main() {
	flag.StringVar(&configFile, "config-file", ".grope.yaml", "Configuaration file")

	flag.Parse()

	cfg, err := loadConfig(configFile)
	if err != nil {
		log.WithError(err).Error("error loading configuration file")
		return
	}

	// initialize logger
	if cfg.LogFile != "" {
		log.SetFormatter(&logrus.JSONFormatter{})
		log.SetOutput(&lumberjack.Logger{
			Filename:   cfg.LogFile,
			MaxSize:    50,
			MaxBackups: 5,
			MaxAge:     28,
			Compress:   true,
		})
	} else {
		log.SetOutput(os.Stdout)
	}

	if len(cfg.Repositories) == 0 {
		log.Error("no repositories to grope")
		return
	}

	if err := cfg.Apply(); err != nil {
		log.Error(err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	distruptSignal := make(chan bool, 1)

	// poll
	go func() {
		ticker := time.NewTicker(cfg.PollTime.Duration)
		for {
			select {
			case <-distruptSignal:
				ticker.Stop()
				cancel()
			case <-ticker.C:
				for _, repo := range cfg.Repositories {
					go repo.FeelAndGrope(ctx)
				}
			}
		}
	}()

	stopChan := make(chan os.Signal, 1)

	// Notify the stopChan when an interrupt or terminate signal is received
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	log.Info("waiting for stop signal...")

	<-stopChan

	log.Info("received shutdown signal. waiting for any groping to end...")
	distruptSignal <- true

	_, waitCtxCancel := context.WithTimeout(ctx, 5*time.Second)
	defer waitCtxCancel()
}

func loadConfig(filename string) (*Config, error) {
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
