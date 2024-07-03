package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gintec-rdl/gitgrope/gitgrope"
	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	configFile string
	log        = logrus.New()
)

func main() {
	flag.StringVar(&configFile, "config-file", ".grope.yaml", "Configuaration file")

	flag.Parse()

	cfg, err := gitgrope.LoadConfig(configFile)
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

	if err := cfg.Apply(log); err != nil {
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
					if cfg.FireOnce {
						log.Info("stopping ticker after firing once")
						ticker.Stop()
					}
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
